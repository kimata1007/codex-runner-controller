package zerotrust

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/zerotrust/cni"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type NodeProxy struct {
	Name           string
	UID            types.UID
	PeerIP         string
	StateDirectory string
	LocalDirectory string
	Routes         RouteResolver
	Issuer         *IssuerClient
}

type routeEnrollment struct {
	reference cni.Attachment
	lifetime  context.Context
	remove    func()
}

type nodeRuntime struct {
	cfg          NodeProxy
	relay        *Relay
	certificates *CertificateManager
	registry     RouteRegistry
	egressSlots  chan struct{}
}

func (p NodeProxy) Run(ctx context.Context) error {
	if p.Name == "" || p.UID == "" || net.ParseIP(p.PeerIP) == nil ||
		!filepath.IsAbs(p.StateDirectory) || !filepath.IsAbs(p.LocalDirectory) || p.Issuer == nil {
		return errors.New("invalid node proxy configuration")
	}
	if err := os.MkdirAll(p.LocalDirectory, 0700); err != nil {
		return err
	}
	config := net.ListenConfig{}
	peerAddr := net.JoinHostPort(p.PeerIP, strconv.Itoa(PeerPort))
	peer, err := config.Listen(ctx, "tcp", peerAddr)
	if err != nil {
		return err
	}
	defer peer.Close()
	control, err := ListenPrivate(filepath.Join(p.LocalDirectory, "runner-api.sock"))
	if err != nil {
		return err
	}
	defer control.Close()

	certificates := NewCertificateManager(p.Issuer)
	bundle := &bundleCache{issuer: p.Issuer, lock: make(chan struct{}, 1)}
	n := nodeRuntime{
		cfg: p, egressSlots: make(chan struct{}, 512), certificates: certificates,
		relay: &Relay{Domain: p.Routes.Policies.Domain, Authority: p.Routes.Policies, Credentials: certificates, Roots: bundle.load},
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var tasks sync.WaitGroup
	defer func() {
		cancel()
		tasks.Wait()
	}()
	for _, next := range []func(){func() { certificates.Renew(ctx) }, func() { n.watchLocal(ctx) }} {
		tasks.Add(1)
		go func() {
			defer tasks.Done()
			next()
		}()
	}
	errs := make(chan error, 2)
	tasks.Add(2)
	go func() {
		defer tasks.Done()
		errs <- ServeConnections(ctx, peer, make(chan struct{}, 512), func(ctx context.Context, conn net.Conn) error {
			return n.relay.Inbound(ctx, conn, n.registry.Resolve)
		})
	}()
	go func() {
		defer tasks.Done()
		errs <- ServeConnections(ctx, control, make(chan struct{}, 128), n.control)
	}()
	slog.InfoContext(ctx, "node proxy listening", "node", p.Name, "address", peerAddr)
	return <-errs
}

func (n *nodeRuntime) watchLocal(ctx context.Context) {
	sessions := make(map[string]routeEnrollment)
	proxies := make(map[CertificateBinding]func())
	defer func() {
		for _, session := range sessions {
			session.remove()
		}
		for _, remove := range proxies {
			remove()
		}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		n.refreshSessions(ctx, sessions)
		n.refreshProxies(ctx, proxies)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (n *nodeRuntime) refreshSessions(ctx context.Context, sessions map[string]routeEnrollment) {
	attachments, _ := cni.ReadAttachments(n.cfg.StateDirectory)
	current := make(map[string]cni.Attachment, len(attachments))
	for _, attachment := range attachments {
		if attachment.Namespace != n.cfg.Routes.Policies.SessionNamespace || attachment.HostIP.String() != n.cfg.PeerIP || !attachment.Live() {
			continue
		}
		if err := cni.CheckFirewall(attachment.Generation); err != nil {
			if detachErr := attachment.Detach(); detachErr != nil {
				slog.ErrorContext(ctx, "cannot isolate session veth", "operation", "network.detach", "pod_uid", attachment.PodUID, "error", errors.Join(err, detachErr))
			}
			continue
		}
		current[attachment.Generation] = attachment
	}
	for generation, enrolled := range sessions {
		if current[generation] != enrolled.reference || enrolled.lifetime.Err() != nil {
			enrolled.remove()
			delete(sessions, generation)
		}
	}
	for generation, attachment := range current {
		if _, exists := sessions[generation]; exists || ctx.Err() != nil {
			continue
		}
		enrollment, err := n.enrollSession(ctx, attachment)
		if err == nil {
			sessions[generation] = enrollment
		}
	}
}

func (n *nodeRuntime) enrollSession(ctx context.Context, attachment cni.Attachment) (routeEnrollment, error) {
	deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	policy, err := n.cfg.Routes.Policies.Lookup(deadline, types.UID(attachment.PodUID), time.Now())
	if err != nil || policy.Workload.PodName != attachment.PodName || policy.Workload.NodeName != n.cfg.Name ||
		policy.Workload.NodeUID != n.cfg.UID {
		return routeEnrollment{}, ErrPolicyDenied
	}
	var pod corev1.Pod
	if err := n.cfg.Routes.Policies.Reader.Get(deadline, client.ObjectKey{Namespace: attachment.Namespace, Name: attachment.PodName}, &pod); err != nil ||
		pod.UID != policy.Workload.PodUID || pod.Status.PodIP != attachment.PodIP.String() {
		return routeEnrollment{}, ErrPolicyDenied
	}
	binding := CertificateBinding{Identity: policy.Identity, PodUID: policy.Workload.PodUID}
	local, stop := context.WithCancel(ctx)
	remove, err := n.registry.Register(local, LocalTarget{Destination: binding, Port: ControlPort,
		Connect: func(ctx context.Context, _ Identity) (net.Conn, error) { return attachment.DialControl(ctx) }})
	if err != nil {
		stop()
		return routeEnrollment{}, err
	}
	var running sync.WaitGroup
	cleanup := func() {
		stop()
		remove()
		n.certificates.Forget(binding)
		running.Wait()
	}
	slots := make(chan struct{}, 24)
	for _, destination := range cni.Destinations() {
		listener, err := attachment.ListenProxy(local, destination)
		if err != nil {
			cleanup()
			return routeEnrollment{}, err
		}
		running.Add(1)
		go func() {
			defer running.Done()
			defer listener.Close()
			if err := ServeConnections(local, listener, slots, func(ctx context.Context, conn net.Conn) error {
				select {
				case n.egressSlots <- struct{}{}:
					defer func() { <-n.egressSlots }()
				default:
					return ErrPolicyDenied
				}
				if !attachment.Live() {
					return ErrPolicyDenied
				}
				proxy := Identity("spiffe://" + n.relay.Domain + "/proxy/" + destination.Role)
				route, err := n.cfg.Routes.Proxy(ctx, binding, proxy)
				if err != nil {
					return err
				}
				return n.relay.Outbound(ctx, conn, route)
			}); err != nil {
				stop()
			}
		}()
	}
	return routeEnrollment{reference: attachment, lifetime: local, remove: cleanup}, nil
}

func (n *nodeRuntime) refreshProxies(ctx context.Context, enrolled map[CertificateBinding]func()) {
	current := make(map[CertificateBinding]bool)
	w := n.cfg.Routes.Trusted
	for _, role := range []string{"github", "web", "model"} {
		name := "codex-" + role + "-proxy"
		pods, err := n.localPods(ctx, w.SecurityNamespace, name)
		if err != nil {
			continue
		}
		for _, workload := range pods {
			binding := workload.Binding
			path := ServiceSocket(n.cfg.LocalDirectory, role, binding.PodUID)
			if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0660 {
				continue
			}
			current[binding] = true
			if _, exists := enrolled[binding]; exists {
				continue
			}
			remove, err := n.registry.Register(ctx, LocalTarget{Destination: binding, Port: ProxyPort,
				Connect: func(ctx context.Context, from Identity) (net.Conn, error) {
					conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
					if err != nil {
						return nil, err
					}
					if err := writePreface(conn, string(from)); err != nil {
						_ = conn.Close()
						return nil, err
					}
					return conn, nil
				}})
			if err == nil {
				enrolled[binding] = remove
			}
		}
	}
	for binding, remove := range enrolled {
		if !current[binding] {
			remove()
			n.certificates.Forget(binding)
			delete(enrolled, binding)
		}
	}
}

func (n *nodeRuntime) localPods(ctx context.Context, namespace, label string) ([]TrustedWorkload, error) {
	w := n.cfg.Routes.Trusted
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var pods corev1.PodList
	if err := w.Reader.List(ctx, &pods, client.InNamespace(namespace),
		client.MatchingFields{"spec.nodeName": n.cfg.Name}, client.MatchingLabels{"app.kubernetes.io/name": label}); err != nil {
		return nil, err
	}
	var workloads []TrustedWorkload
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !runningPod(pod) || !isPodReady(pod) {
			continue
		}
		workload, err := w.Lookup(ctx, PodReference{Namespace: namespace, Name: pod.Name, UID: pod.UID})
		if err == nil && workload.Pod.Spec.NodeName == n.cfg.Name {
			workloads = append(workloads, workload)
		}
	}
	return workloads, nil
}

func (n *nodeRuntime) control(ctx context.Context, conn net.Conn) error {
	reader, uid, err := routePreface(conn)
	if err != nil || !component.MatchString(uid) {
		return ErrPolicyDenied
	}
	runners, err := n.localPods(ctx, n.cfg.Routes.Trusted.APINamespace, "codex-runner-api")
	if err != nil || len(runners) == 0 {
		return ErrPolicyDenied
	}
	route, err := n.cfg.Routes.Session(ctx, runners[0].Binding, types.UID(uid))
	if err != nil {
		return err
	}
	return n.relay.Outbound(ctx, &localConnection{Conn: conn, reader: reader}, route)
}

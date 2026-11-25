package zerotrust

import (
	"context"
	"net"
	"net/netip"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const PeerPort = 9443

// RouteResolver maps authorized destinations to a live destination Node. It
// never dials a Pod IP on behalf of the originating workload.
type RouteResolver struct {
	Policies PolicyStore
	Trusted  TrustedWorkloads
}

func (r RouteResolver) Session(ctx context.Context, source CertificateBinding, target types.UID) (Route, error) {
	now := time.Now()
	permission, err := r.Policies.Lookup(ctx, target, now)
	if err != nil || !permission.Policy.Allows(source.Identity, permission.Identity, ControlPort, time.Now()) {
		return Route{}, ErrPolicyDenied
	}
	node, ok := readLiveNode(ctx, r.Policies.Reader, permission.Workload.NodeName, permission.Workload.NodeUID)
	if !ok {
		return Route{}, ErrPolicyDenied
	}
	host, ok := peerAddress(&node)
	if !ok {
		return Route{}, ErrPolicyDenied
	}
	return Route{Source: source, Destination: permission.Identity, Address: host, Port: ControlPort}, nil
}

func (r RouteResolver) Proxy(ctx context.Context, source CertificateBinding, destination Identity) (Route, error) {
	if _, err := r.Policies.AuthorizeRoute(ctx, source.Identity, destination, ProxyPort); err != nil {
		return Route{}, err
	}
	application, ok := r.Trusted.proxyDeployment(destination)
	if !ok || r.Trusted.Reader == nil {
		return Route{}, ErrPolicyDenied
	}
	var pods corev1.PodList
	if err := r.Trusted.Reader.List(ctx, &pods, client.InNamespace(application.Namespace),
		client.MatchingLabels{"app.kubernetes.io/name": application.Name}); err != nil {
		return Route{}, ErrPolicyDenied
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !runningPod(pod) || !isPodReady(pod) {
			continue
		}
		workload, err := r.Trusted.Lookup(ctx, PodReference{Namespace: pod.Namespace, Name: pod.Name, UID: pod.UID})
		if err != nil || workload.Binding.Identity != destination {
			continue
		}
		node, ok := readLiveNode(ctx, r.Trusted.Reader, workload.Pod.Spec.NodeName, "")
		if ok {
			if address, ok := peerAddress(&node); ok {
				return Route{Source: source, Destination: destination, Port: ProxyPort, Address: address}, nil
			}
		}
	}
	return Route{}, ErrPolicyDenied
}

func isPodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func peerAddress(node *corev1.Node) (string, bool) {
	for _, address := range node.Status.Addresses {
		if address.Type != corev1.NodeInternalIP {
			continue
		}
		ip, err := netip.ParseAddr(address.Address)
		if err == nil && ip.IsGlobalUnicast() && !ip.IsLinkLocalUnicast() && ip.Zone() == "" {
			return net.JoinHostPort(ip.String(), strconv.Itoa(PeerPort)), true
		}
	}
	return "", false
}

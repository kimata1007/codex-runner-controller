package main

import (
	"context"
	"crypto/x509"
	"errors"
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/logging"
	"github.com/kimata1007/codex-runner-controller/internal/zerotrust"
	"github.com/kimata1007/codex-runner-controller/internal/zerotrust/cni"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func main() {
	slog.SetDefault(logging.New("node-proxy"))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("node proxy stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	domain := flag.String("trust-domain", "codex.local", "SPIFFE trust domain")
	nodeName := flag.String("node-name", os.Getenv("POD_NODE_NAME"), "local Kubernetes Node name")
	peerIP := flag.String("peer-ip", os.Getenv("POD_HOST_IP"), "Kubernetes Node internal address")
	issuerURL := flag.String("issuer", "https://codex-identity-issuer.codex-security.svc:8443", "identity issuer HTTPS origin")
	issuerTrust := flag.String("issuer-trust", "/var/run/codex/issuer-trust/ca.crt", "issuer HTTPS trust root")
	issuerToken := flag.String("issuer-token", "/var/run/codex/issuer-token/token", "projected issuer audience token")
	policyNamespace := flag.String("policy-namespace", "codex-security", "SessionPolicy namespace")
	sessionNamespace := flag.String("session-namespace", "codex-sessions", "Session Pod namespace")
	apiNamespace := flag.String("runner-api-namespace", "codex-system", "Runner API namespace")
	runtimeClass := flag.String("runtime-class", "codex-gvisor", "required Session Pod RuntimeClass")
	state := flag.String("cni-state-dir", cni.StateDirectory, "trusted host CNI attachment directory")
	local := flag.String("socket-dir", zerotrust.NodeRuntimeDirectory, "trusted local socket directory")
	installConfig := flag.String("install-cni", "", "install CNI binary and extend this absolute conflist, then exit")
	flag.Parse()
	if *installConfig != "" {
		return installCNI(*installConfig, *state, *local, *sessionNamespace)
	}
	if *nodeName == "" || net.ParseIP(*peerIP) == nil || *runtimeClass == "" ||
		*sessionNamespace == "" || *policyNamespace == "" || *apiNamespace == "" ||
		*sessionNamespace == *policyNamespace || *sessionNamespace == *apiNamespace {
		return errors.New("invalid node proxy configuration")
	}
	pem, err := os.ReadFile(*issuerTrust)
	roots := x509.NewCertPool()
	if err != nil || len(pem) > 64<<10 || !roots.AppendCertsFromPEM(pem) {
		return errors.New("identity issuer HTTPS trust root is unavailable")
	}
	issuer, err := zerotrust.NewIssuerClient(*issuerURL, *domain, roots, func() (string, error) {
		token, err := os.ReadFile(*issuerToken)
		return string(token), err
	})
	if err != nil {
		return err
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	config.Timeout = 2 * time.Second
	config.QPS, config.Burst = 100, 150
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, appsv1.AddToScheme, v1alpha1.AddToScheme} {
		if err := add(scheme); err != nil {
			return err
		}
	}
	kube, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	var node corev1.Node
	if err := kube.Get(ctx, client.ObjectKey{Name: *nodeName}, &node); err != nil || node.DeletionTimestamp != nil {
		return errors.New("local Kubernetes Node is unavailable")
	}
	localAddress := false
	for _, address := range node.Status.Addresses {
		if address.Type == corev1.NodeInternalIP && address.Address == *peerIP {
			localAddress = true
			break
		}
	}
	if !localAddress {
		return errors.New("peer listener is not a Kubernetes Node internal address")
	}
	proxy := zerotrust.NodeProxy{
		Name: *nodeName, UID: node.UID, PeerIP: *peerIP, StateDirectory: *state, LocalDirectory: *local, Issuer: issuer,
		Routes: zerotrust.RouteResolver{
			Policies: zerotrust.PolicyStore{
				Reader: kube, Domain: *domain, PolicyNamespace: *policyNamespace, SessionNamespace: *sessionNamespace, RuntimeClass: *runtimeClass,
			},
			Trusted: zerotrust.TrustedWorkloads{Reader: kube, Domain: *domain, APINamespace: *apiNamespace, SecurityNamespace: *policyNamespace},
		},
	}
	return proxy.Run(ctx)
}

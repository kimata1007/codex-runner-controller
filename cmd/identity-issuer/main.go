package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/logging"
	"github.com/kimata1007/codex-runner-controller/internal/zerotrust"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	authentication "k8s.io/client-go/kubernetes/typed/authentication/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func main() {
	slog.SetDefault(logging.New("identity-issuer"))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("identity issuer stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	address := flag.String("listen", ":8443", "HTTPS listen address")
	domain := flag.String("trust-domain", "codex.local", "SPIFFE trust domain")
	caDirectory := flag.String("ca-dir", "/var/run/codex/issuer-ca", "mounted signing key and trust bundle directory")
	tlsDirectory := flag.String("tls-dir", "/var/run/codex/issuer-tls", "mounted HTTPS key pair directory")
	policyNamespace := flag.String("policy-namespace", "codex-security", "SessionPolicy namespace")
	sessionNamespace := flag.String("session-namespace", "codex-sessions", "isolated Session Pod namespace")
	runtimeClass := flag.String("runtime-class", "codex-gvisor", "required Session Pod runtime class")
	nodeNamespace := flag.String("node-proxy-namespace", "codex-security", "Node Proxy DaemonSet namespace")
	nodeServiceAccount := flag.String("node-proxy-service-account", "codex-node-proxy", "Node Proxy ServiceAccount name")
	nodeDaemonSet := flag.String("node-proxy-daemonset", "codex-node-proxy", "Node Proxy DaemonSet name")
	ttl := flag.Duration("certificate-ttl", zerotrust.DefaultCertificateTTL, "workload certificate lifetime")
	flag.Parse()
	if _, err := zerotrust.ParseIdentity("spiffe://"+*domain+"/system/issuer", *domain); err != nil {
		return err
	}
	if *policyNamespace == "" || *sessionNamespace == "" || *policyNamespace == *sessionNamespace ||
		*runtimeClass == "" || *nodeNamespace == "" || *nodeNamespace == *sessionNamespace ||
		*nodeServiceAccount == "" || *nodeDaemonSet == "" || *ttl < time.Second || *ttl > 10*time.Minute {
		return errors.New("invalid identity issuer configuration")
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	config.Timeout = 3 * time.Second
	config.QPS, config.Burst = 200, 300
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, appsv1.AddToScheme, v1alpha1.AddToScheme} {
		if err := add(scheme); err != nil {
			return err
		}
	}
	reader, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	auth, err := authentication.NewForConfig(config)
	if err != nil {
		return err
	}
	ca := zerotrust.CAFiles{Directory: *caDirectory, Domain: *domain, TTL: *ttl}
	nodes := zerotrust.NodeAuthenticator{
		Tokens: auth.TokenReviews(), Reader: reader, Namespace: *nodeNamespace,
		ServiceAccount: *nodeServiceAccount, DaemonSet: *nodeDaemonSet,
	}
	policies := zerotrust.PolicyStore{
		Reader: reader, Domain: *domain, PolicyNamespace: *policyNamespace,
		SessionNamespace: *sessionNamespace, RuntimeClass: *runtimeClass,
	}
	server := &http.Server{
		Addr:              *address,
		Handler:           zerotrust.NewIssuerHandler(ca, nodes, policies),
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       8 * time.Second,
		WriteTimeout:      12 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
		TLSConfig: &tls.Config{
			MinVersion:             tls.VersionTLS13,
			MaxVersion:             tls.VersionTLS13,
			SessionTicketsDisabled: true,
			GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
				pair, err := tls.LoadX509KeyPair(filepath.Join(*tlsDirectory, "tls.crt"), filepath.Join(*tlsDirectory, "tls.key"))
				if err != nil {
					return nil, errors.New("issuer HTTPS key pair unavailable")
				}
				return &pair, nil
			},
		},
	}
	shutdown := make(chan error, 1)
	go func() {
		<-ctx.Done()
		deadline, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdown <- server.Shutdown(deadline)
	}()
	slog.InfoContext(ctx, "identity issuer listening", "address", *address)
	err = server.ListenAndServeTLS("", "")
	if errors.Is(err, http.ErrServerClosed) {
		return <-shutdown
	}
	return err
}

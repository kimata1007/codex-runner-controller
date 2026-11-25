package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/logging"
	"github.com/kimata1007/codex-runner-controller/internal/modelproxy"
	"github.com/kimata1007/codex-runner-controller/internal/zerotrust"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func main() {
	slog.SetDefault(logging.New("model-proxy"))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("model proxy stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	domain := flag.String("trust-domain", "codex.local", "SPIFFE trust domain")
	policies := flag.String("policy-namespace", "codex-security", "SessionPolicy namespace")
	sessions := flag.String("session-namespace", "codex-sessions", "Session Pod namespace")
	runtimeClass := flag.String("runtime-class", "codex-gvisor", "required Session Pod runtime class")
	usage := flag.String("usage-namespace", "codex-model-usage", "model budget storage namespace")
	credential := flag.String("credential-file", "/var/run/codex/openai/api-key", "mounted OpenAI API key")
	health := flag.Bool("healthcheck", false, "check the local listener")
	flag.Parse()
	uid := types.UID(os.Getenv("POD_UID"))
	if uid == "" {
		return errors.New("model proxy needs a Kubernetes Pod UID")
	}
	socket := zerotrust.ServiceSocket(zerotrust.NodeRuntimeDirectory, "model", uid)
	if *health {
		conn, err := net.DialTimeout("unix", socket, time.Second)
		if err != nil {
			return err
		}
		return conn.Close()
	}
	if _, err := zerotrust.ParseIdentity("spiffe://"+*domain+"/proxy/model", *domain); err != nil {
		return err
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	config.Timeout = 3 * time.Second
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return err
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		return err
	}
	kube, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	store := zerotrust.PolicyStore{Reader: kube, Domain: *domain, PolicyNamespace: *policies, SessionNamespace: *sessions, RuntimeClass: *runtimeClass}
	handler := modelproxy.New(store, modelproxy.BudgetStore{Kube: kube, Namespace: *usage}, func() (string, error) {
		value, err := os.ReadFile(*credential)
		if err != nil || len(value) > 4096 {
			return "", errors.New("model credential unavailable")
		}
		return string(value), nil
	}, *domain)
	listener, err := zerotrust.ListenPrivate(socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{Handler: handler, ConnContext: zerotrust.ContextWithLocalIdentity,
		ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 8 * time.Second, WriteTimeout: 13 * time.Minute,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 1)
	go func() { done <- server.Serve(zerotrust.AuthenticatedListener(listener, *domain)) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}
		<-done
		return nil
	}
}

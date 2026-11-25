package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kimata1007/codex-runner-controller/internal/logging"
	"github.com/kimata1007/codex-runner-controller/internal/webproxy"
	"github.com/kimata1007/codex-runner-controller/internal/zerotrust"
	"k8s.io/apimachinery/pkg/types"
)

func main() {
	slog.SetDefault(logging.New("web-proxy"))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("web proxy stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	domain := flag.String("trust-domain", "codex.local", "SPIFFE trust domain")
	policies := flag.String("policy-namespace", "codex-security", "SessionPolicy namespace")
	sessions := flag.String("session-namespace", "codex-sessions", "Session Pod namespace")
	runtimeClass := flag.String("runtime-class", "codex-gvisor", "required Session Pod runtime class")
	credential := flag.String("credential-file", "/var/run/codex/brave/api-key", "mounted Brave Search API key")
	health := flag.Bool("healthcheck", false, "check the local listener")
	flag.Parse()
	socket, err := zerotrust.ServicePath("web", *domain, types.UID(os.Getenv("POD_UID")))
	if err != nil {
		return err
	}
	if *health {
		return zerotrust.CheckServiceSocket(socket)
	}
	kube, err := zerotrust.InClusterProxyClient()
	if err != nil {
		return err
	}
	store := zerotrust.PolicyStore{Reader: kube, Domain: *domain, PolicyNamespace: *policies, SessionNamespace: *sessions, RuntimeClass: *runtimeClass}
	handler := webproxy.New(store, *domain, webproxy.NewBraveSearch(zerotrust.MountedCredential(*credential)))
	return zerotrust.ServeProxy(ctx, socket, *domain, handler)
}

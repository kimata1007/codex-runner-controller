package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kimata1007/codex-runner-controller/internal/githubproxy"
	"github.com/kimata1007/codex-runner-controller/internal/logging"
	"github.com/kimata1007/codex-runner-controller/internal/zerotrust"
	"k8s.io/apimachinery/pkg/types"
)

func main() {
	slog.SetDefault(logging.New("github-proxy"))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("github proxy stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	domain := flag.String("trust-domain", "codex.local", "SPIFFE trust domain")
	policies := flag.String("policy-namespace", "codex-security", "SessionPolicy namespace")
	sessions := flag.String("session-namespace", "codex-sessions", "Session Pod namespace")
	runtimeClass := flag.String("runtime-class", "codex-gvisor", "required Session Pod runtime class")
	appID := flag.String("app-id", os.Getenv("GITHUB_APP_ID"), "GitHub App identifier")
	keyFile := flag.String("app-key-file", "/var/run/codex/github/private-key.pem", "mounted GitHub App signing key")
	health := flag.Bool("healthcheck", false, "check the local listener")
	flag.Parse()
	socket, err := zerotrust.ServicePath("github", *domain, types.UID(os.Getenv("POD_UID")))
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
	app, err := githubproxy.NewApp(*appID, func() ([]byte, error) { return os.ReadFile(*keyFile) })
	if err != nil {
		return err
	}
	store := zerotrust.PolicyStore{Reader: kube, Domain: *domain, PolicyNamespace: *policies, SessionNamespace: *sessions, RuntimeClass: *runtimeClass}
	return zerotrust.ServeProxy(ctx, socket, *domain, githubproxy.New(app, store, *domain))
}

package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/controller"
	"github.com/kimata1007/codex-runner-controller/internal/kubernetes"
	"github.com/kimata1007/codex-runner-controller/internal/logging"
	"github.com/kimata1007/codex-runner-controller/internal/telemetry"
)

func main() {
	logger := logging.New("runner-controller")
	slog.SetDefault(logger)
	shutdown, err := telemetry.Start(context.Background(), "runner-controller")
	if err != nil {
		slog.Error("failed to initialize tracing", "operation", "startup.tracing", "error.type", logging.ErrorType(err))
		os.Exit(1)
	}
	defer telemetry.Shutdown(context.Background(), shutdown)
	ctrl.SetLogger(logr.FromSlogHandler(logger.Handler()))
	runnerImage := os.Getenv("RUNNER_AGENT_IMAGE")
	if runnerImage == "" {
		slog.Error("RUNNER_AGENT_IMAGE must be set to a pinned image")
		os.Exit(1)
	}

	namespace := os.Getenv("KUBERNETES_NAMESPACE")
	if namespace == "" {
		slog.Error("KUBERNETES_NAMESPACE must be set")
		os.Exit(1)
	}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		slog.Error("failed to add core scheme", "operation", "controller.add_core_scheme", "error.type", logging.ErrorType(err))
		os.Exit(1)
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		slog.Error("failed to add CodexSession scheme", "operation", "controller.add_codexsession_scheme", "error.type", logging.ErrorType(err))
		os.Exit(1)
	}
	mgr, err := manager.New(ctrl.GetConfigOrDie(), manager.Options{
		Scheme: scheme,
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{namespace: {}},
		},
	})
	if err != nil {
		slog.Error("failed to create manager", "operation", "controller.create_manager", "error.type", logging.ErrorType(err))
		os.Exit(1)
	}
	serviceAccountName := os.Getenv("RUNNER_SERVICE_ACCOUNT_NAME")
	if serviceAccountName == "" {
		serviceAccountName = kubernetes.DefaultRunnerServiceAccount
	}
	tokenSecretKey := os.Getenv("RUNNER_AGENT_TOKEN_SECRET_KEY")
	if tokenSecretKey == "" {
		tokenSecretKey = "agent-token"
	}
	apiTokenSecretKey := os.Getenv("RUNNER_API_TOKEN_SECRET_KEY")
	if apiTokenSecretKey == "" {
		apiTokenSecretKey = "api-token"
	}
	builder := kubernetes.ResourceBuilder{
		RunnerImage:                runnerImage,
		OTLPEndpoint:               os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		RunnerEventURL:             os.Getenv("RUNNER_EVENT_URL"),
		RunnerHeartbeatURL:         os.Getenv("RUNNER_HEARTBEAT_URL"),
		RunnerAuthRefreshURL:       os.Getenv("RUNNER_AUTH_REFRESH_URL"),
		RunnerAgentTokenSecretName: os.Getenv("RUNNER_AGENT_TOKEN_SECRET_NAME"),
		RunnerAgentTokenSecretKey:  tokenSecretKey,
		CodexAccessTokenSecretName: os.Getenv("CODEX_ACCESS_TOKEN_SECRET_NAME"),
		CodexAccessTokenSecretKey:  os.Getenv("CODEX_ACCESS_TOKEN_SECRET_KEY"),
		RunnerAPITokenSecretName:   os.Getenv("RUNNER_API_TOKEN_SECRET_NAME"),
		RunnerAPITokenSecretKey:    apiTokenSecretKey,
		RunnerServiceAccountName:   serviceAccountName,
	}
	if err := (&controller.CodexSessionController{Client: mgr.GetClient(), Builder: builder}).SetupWithManager(mgr); err != nil {
		slog.Error("failed to set up controller", "operation", "controller.setup", "error.type", logging.ErrorType(err))
		os.Exit(1)
	}
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		slog.Error("failed to start manager", "operation", "controller.start", "error.type", logging.ErrorType(err))
		os.Exit(1)
	}
}

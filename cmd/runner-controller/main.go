package main

import (
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/controller"
	"github.com/kimata1007/codex-runner-controller/internal/kubernetes"
)

func main() {
	logger := zap.New(zap.UseDevMode(true))
	ctrl.SetLogger(logger)
	runnerImage := os.Getenv("RUNNER_AGENT_IMAGE")
	if runnerImage == "" {
		logger.Error(nil, "RUNNER_AGENT_IMAGE must be set to a pinned image")
		os.Exit(1)
	}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		logger.Error(err, "add core scheme")
		os.Exit(1)
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		logger.Error(err, "add CodexSession scheme")
		os.Exit(1)
	}
	mgr, err := manager.New(ctrl.GetConfigOrDie(), manager.Options{Scheme: scheme})
	if err != nil {
		logger.Error(err, "create manager")
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
		RunnerEventURL:             os.Getenv("RUNNER_EVENT_URL"),
		RunnerHeartbeatURL:         os.Getenv("RUNNER_HEARTBEAT_URL"),
		RunnerAgentTokenSecretName: os.Getenv("RUNNER_AGENT_TOKEN_SECRET_NAME"),
		RunnerAgentTokenSecretKey:  tokenSecretKey,
		CodexAccessTokenSecretName: os.Getenv("CODEX_ACCESS_TOKEN_SECRET_NAME"),
		CodexAccessTokenSecretKey:  os.Getenv("CODEX_ACCESS_TOKEN_SECRET_KEY"),
		RunnerAPITokenSecretName:   os.Getenv("RUNNER_API_TOKEN_SECRET_NAME"),
		RunnerAPITokenSecretKey:    apiTokenSecretKey,
		RunnerServiceAccountName:   serviceAccountName,
	}
	if err := (&controller.CodexSessionController{Client: mgr.GetClient(), Builder: builder}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "setup controller")
		os.Exit(1)
	}
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "start manager")
		os.Exit(1)
	}
}

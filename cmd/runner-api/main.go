package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/agent"
	"github.com/kimata1007/codex-runner-controller/internal/agentclient"
	"github.com/kimata1007/codex-runner-controller/internal/auth"
	"github.com/kimata1007/codex-runner-controller/internal/authbroker"
	"github.com/kimata1007/codex-runner-controller/internal/conversation"
	"github.com/kimata1007/codex-runner-controller/internal/events"
	"github.com/kimata1007/codex-runner-controller/internal/kubernetes"
	"github.com/kimata1007/codex-runner-controller/internal/libsql"
	"github.com/kimata1007/codex-runner-controller/internal/logging"
	"github.com/kimata1007/codex-runner-controller/internal/memory"
	"github.com/kimata1007/codex-runner-controller/internal/runnerapi"
	"github.com/kimata1007/codex-runner-controller/internal/runs"
	"github.com/kimata1007/codex-runner-controller/internal/session"
	"github.com/kimata1007/codex-runner-controller/internal/telemetry"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

func main() {
	slog.SetDefault(logging.New("runner-api"))
	shutdown, err := telemetry.Start(context.Background(), "runner-api")
	if err != nil {
		slog.Error("failed to initialize tracing", "operation", "startup.tracing", "error.type", logging.ErrorType(err))
		os.Exit(1)
	}
	defer telemetry.Shutdown(context.Background(), shutdown)
	addr := os.Getenv("RUNNER_API_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	migrationContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	repository, err := repositoryFromEnvironment(migrationContext)
	cancel()
	if err != nil {
		slog.Error("failed to initialize repository", "operation", "startup.repository", "error.type", logging.ErrorType(err))
		os.Exit(1)
	}
	dispatcher, activator, authBroker, err := runtimeFromEnvironment(repository)
	if err != nil {
		slog.Error("failed to initialize runtime", "operation", "startup.runtime", "error.type", logging.ErrorType(err))
		os.Exit(1)
	}
	dependencies := runnerapi.HandlerDependencies{Repository: repository, Dispatcher: dispatcher, Activator: activator, AuthBroker: authBroker}
	if eventStore, ok := repository.(runnerapi.RunEventStore); ok {
		dependencies.EventStore = eventStore
		dependencies.EventHub = events.NewBroker()
	}
	apiHandler := runnerapi.NewHandler(dependencies)
	var httpHandler http.Handler = runnerapi.NewRouter(apiHandler)
	httpHandler = auth.BearerTokenForPathPrefix(httpHandler, os.Getenv("RUNNER_API_TOKEN"), "/internal/runs/")
	httpHandler = auth.BearerTokenForPathPrefix(httpHandler, os.Getenv("RUNNER_AGENT_TOKEN"), "/internal/sessions/")
	httpHandler = auth.BearerTokenForPathPrefix(httpHandler, os.Getenv("RUNNER_AGENT_TOKEN"), "/internal/auth/")
	httpHandler = telemetry.HTTPHandler(httpHandler)
	server := &http.Server{
		Addr:              addr,
		Handler:           httpHandler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		// The run-event endpoint uses long-lived SSE responses. A fixed
		// WriteTimeout would close valid streams while a turn is still running.
		IdleTimeout: 60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if store, ok := repository.(conversation.RetentionStore); ok {
		var cleaner runnerapi.ConversationSessionCleaner
		if value, ok := activator.(runnerapi.ConversationSessionCleaner); ok {
			cleaner = value
		}
		go runnerapi.RunConversationArchiver(ctx, store, cleaner)
	}
	go apiHandler.RunQueueWorker(ctx, 0)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("HTTP server shutdown failed", "operation", "http.server_shutdown", "error.type", logging.ErrorType(err))
		}
	}()

	slog.Info("HTTP server listening", "address", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("HTTP server failed", "operation", "http.server_start", "error.type", logging.ErrorType(err))
		os.Exit(1)
	}
}

func runtimeFromEnvironment(repository conversation.Repository) (runnerapi.MessageDispatcher, session.Activator, runnerapi.ChatGPTAuthBroker, error) {
	agentURL := os.Getenv("RUNNER_AGENT_URL")
	runStore, ok := repository.(runs.Store)
	if !ok {
		if agentURL != "" {
			return nil, nil, nil, errors.New("RUNNER_AGENT_URL requires a run-capable libSQL repository")
		}
		return nil, nil, nil, nil
	}
	params := agent.ThreadStartParams{
		Model:          os.Getenv("CODEX_MODEL"),
		CWD:            os.Getenv("CODEX_CWD"),
		ApprovalPolicy: os.Getenv("CODEX_APPROVAL_POLICY"),
	}
	if params.ApprovalPolicy == "" {
		params.ApprovalPolicy = kubernetes.DefaultApprovalPolicy
	}
	var resolver agentclient.EndpointResolver
	var activator session.Activator
	var kubeClient client.Client
	if agentURL != "" {
		resolver = agentclient.StaticEndpointResolver{URL: agentURL}
	} else {
		namespace := os.Getenv("RUNNER_SESSION_NAMESPACE")
		if namespace == "" {
			namespace = kubernetes.DefaultSessionNamespace
		}
		kubeScheme := runtime.NewScheme()
		if err := kubernetes.AddToScheme(kubeScheme); err != nil {
			return nil, nil, nil, fmt.Errorf("register Kubernetes types: %w", err)
		}
		kubeConfig, err := config.GetConfig()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("load Kubernetes configuration for session activation: %w", err)
		}
		kubeClient, err = client.New(kubeConfig, client.Options{Scheme: kubeScheme})
		if err != nil {
			return nil, nil, nil, fmt.Errorf("create Kubernetes client for session activation: %w", err)
		}
		activator = &kubernetes.SessionActivator{
			Client:             kubeClient,
			Namespace:          namespace,
			SessionDataSize:    os.Getenv("RUNNER_SESSION_DATA_SIZE"),
			StorageClassName:   os.Getenv("RUNNER_SESSION_STORAGE_CLASS"),
			CredentialsRefName: gitCredentialsSecretNameFromEnvironment(),
			Model:              params.Model,
			ApprovalPolicy:     params.ApprovalPolicy,
			ReadyTimeout:       kubernetes.DefaultSessionReadyTimeout,
		}
		resolver = kubernetes.SessionEndpointResolver{Namespace: namespace}
	}
	var authTokens runnerapi.ChatGPTAuthBroker
	if encodedKey := os.Getenv("RUNNER_AUTH_ENCRYPTION_KEY"); encodedKey != "" {
		if kubeClient == nil {
			return nil, nil, nil, errors.New("ChatGPT auth broker requires Kubernetes session endpoints")
		}
		key, err := authbroker.ParseEncryptionKey(encodedKey)
		if err != nil {
			return nil, nil, nil, err
		}
		store := kubernetes.ChatGPTAuthSecretStore{Client: kubeClient, Namespace: namespaceFromEnvironment()}
		broker, err := authbroker.New(store, key, nil)
		if err != nil {
			return nil, nil, nil, err
		}
		authURL := strings.TrimSpace(os.Getenv("RUNNER_AUTH_BROKER_URL"))
		if authURL == "" {
			authURL = "http://runner-auth-broker." + namespaceFromEnvironment() + ".svc.cluster.local:8081"
		}
		broker.WithDeviceLoginAgent(agentclient.NewClientWithToken(authURL, os.Getenv("RUNNER_AGENT_TOKEN")))
		authTokens = broker
	}
	factory := agentclient.NewRunClientFactoryWithToken(resolver, params, os.Getenv("RUNNER_AGENT_TOKEN"))
	if authTokens != nil {
		factory.WithAuthTokenProvider(authTokens)
	}
	return runs.NewMessageDispatcherWithFactory(runStore, factory), activator, authTokens, nil
}

func namespaceFromEnvironment() string {
	namespace := strings.TrimSpace(os.Getenv("RUNNER_SESSION_NAMESPACE"))
	if namespace == "" {
		namespace = strings.TrimSpace(os.Getenv("KUBERNETES_NAMESPACE"))
	}
	if namespace == "" {
		namespace = kubernetes.DefaultSessionNamespace
	}
	return namespace
}

func gitCredentialsSecretNameFromEnvironment() string {
	return strings.TrimSpace(os.Getenv("CODEX_GIT_CREDENTIALS_SECRET_NAME"))
}

func repositoryFromEnvironment(ctx context.Context) (conversation.Repository, error) {
	libSQLURL := os.Getenv("LIBSQL_URL")
	if libSQLURL == "" {
		return memory.NewRepository(), nil
	}

	client := libsql.NewClient(libSQLURL, os.Getenv("LIBSQL_AUTH_TOKEN"))
	repository := libsql.NewRepository(client, os.Getenv("RUNNER_API_USER_ID"))
	if err := repository.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("database migration: %w", err)
	}
	return repository, nil
}

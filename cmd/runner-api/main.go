package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/agent"
	"github.com/kimata1007/codex-runner-controller/internal/agentclient"
	"github.com/kimata1007/codex-runner-controller/internal/conversation"
	"github.com/kimata1007/codex-runner-controller/internal/events"
	"github.com/kimata1007/codex-runner-controller/internal/libsql"
	"github.com/kimata1007/codex-runner-controller/internal/memory"
	"github.com/kimata1007/codex-runner-controller/internal/runnerapi"
	"github.com/kimata1007/codex-runner-controller/internal/runs"
	sessionruntime "github.com/kimata1007/codex-runner-controller/internal/session"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

func main() {
	addr := os.Getenv("RUNNER_API_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	migrationContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	repository, err := repositoryFromEnvironment(migrationContext)
	cancel()
	if err != nil {
		log.Fatal(err)
	}
	dispatcher, activator, err := runtimeFromEnvironment(repository)
	if err != nil {
		log.Fatal(err)
	}
	dependencies := runnerapi.HandlerDependencies{Repository: repository, Dispatcher: dispatcher, Activator: activator}
	if eventStore, ok := repository.(runnerapi.RunEventStore); ok {
		dependencies.EventStore = eventStore
		dependencies.EventHub = events.NewBroker()
	}
	apiHandler := runnerapi.NewHandler(dependencies)
	server := &http.Server{
		Addr:              addr,
		Handler:           runnerapi.NewRouter(apiHandler),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("server shutdown: %v", err)
		}
	}()

	log.Printf("runner-api listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func runtimeFromEnvironment(repository conversation.Repository) (runnerapi.MessageDispatcher, runnerapi.SessionActivator, error) {
	agentURL := os.Getenv("RUNNER_AGENT_URL")
	runStore, ok := repository.(runs.Store)
	if !ok {
		if agentURL != "" {
			return nil, nil, errors.New("RUNNER_AGENT_URL requires a run-capable libSQL repository")
		}
		return nil, nil, nil
	}
	params := agent.ThreadStartParams{
		Model:          os.Getenv("CODEX_MODEL"),
		CWD:            os.Getenv("CODEX_CWD"),
		ApprovalPolicy: os.Getenv("CODEX_APPROVAL_POLICY"),
	}
	if params.ApprovalPolicy == "" {
		params.ApprovalPolicy = sessionruntime.DefaultApprovalPolicy
	}
	var resolver agentclient.EndpointResolver
	var activator runnerapi.SessionActivator
	if agentURL != "" {
		resolver = agentclient.StaticEndpointResolver{URL: agentURL}
	} else {
		namespace := os.Getenv("RUNNER_SESSION_NAMESPACE")
		if namespace == "" {
			namespace = sessionruntime.DefaultNamespace
		}
		kubeScheme := runtime.NewScheme()
		if err := sessionruntime.AddToScheme(kubeScheme); err != nil {
			return nil, nil, fmt.Errorf("register Kubernetes types: %w", err)
		}
		kubeConfig, err := config.GetConfig()
		if err != nil {
			return nil, nil, fmt.Errorf("load Kubernetes configuration for session activation: %w", err)
		}
		kubeClient, err := client.New(kubeConfig, client.Options{Scheme: kubeScheme})
		if err != nil {
			return nil, nil, fmt.Errorf("create Kubernetes client for session activation: %w", err)
		}
		activator = &sessionruntime.Activator{
			Client:           kubeClient,
			Namespace:        namespace,
			SessionDataSize:  os.Getenv("RUNNER_SESSION_DATA_SIZE"),
			StorageClassName: os.Getenv("RUNNER_SESSION_STORAGE_CLASS"),
			Model:            params.Model,
			ApprovalPolicy:   params.ApprovalPolicy,
		}
		resolver = agentclient.ServiceEndpointResolver{Namespace: namespace}
	}
	factory := agentclient.NewRunClientFactory(resolver, params)
	return runs.NewMessageDispatcherWithFactory(runStore, factory), activator, nil
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

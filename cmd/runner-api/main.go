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

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/agent"
	"github.com/kimata1007/codex-runner-controller/internal/agentclient"
	"github.com/kimata1007/codex-runner-controller/internal/auth"
	"github.com/kimata1007/codex-runner-controller/internal/conversation"
	"github.com/kimata1007/codex-runner-controller/internal/events"
	kube "github.com/kimata1007/codex-runner-controller/internal/kubernetes"
	"github.com/kimata1007/codex-runner-controller/internal/libsql"
	"github.com/kimata1007/codex-runner-controller/internal/memory"
	"github.com/kimata1007/codex-runner-controller/internal/runnerapi"
	"github.com/kimata1007/codex-runner-controller/internal/runs"
	"github.com/kimata1007/codex-runner-controller/internal/session"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	dispatcher, err := messageDispatcherFromEnvironment(repository)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           auth.BearerToken(handlerFromRepositoryWithActivator(repository, dispatcher, sessionActivatorFromEnvironment()), os.Getenv("RUNNER_API_TOKEN")),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
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

func handlerFromRepository(repository conversation.Repository, dispatcher runnerapi.MessageDispatcher) http.Handler {
	return handlerFromRepositoryWithActivator(repository, dispatcher, nil)
}

func handlerFromRepositoryWithActivator(repository conversation.Repository, dispatcher runnerapi.MessageDispatcher, activator runnerapi.SessionActivator) http.Handler {
	eventStore, canAppend := repository.(runnerapi.RunEventStore)
	broker := events.NewBroker()
	var handler *runnerapi.Handler
	if canAppend {
		handler = runnerapi.NewHandlerWithDispatcherEventsAndActivator(repository, dispatcher, eventStore, broker, activator)
	} else {
		handler = runnerapi.NewHandlerWithDispatcherEventsAndActivator(repository, dispatcher, nil, nil, activator)
	}
	router := runnerapi.NewRouter(handler)
	listStore, canList := repository.(events.Store)
	if !canAppend || !canList {
		return router
	}
	mux := http.NewServeMux()
	mux.Handle("/v1/runs/", events.NewSSEHandler(listStore, broker))
	mux.Handle("/", router)
	return mux
}

func messageDispatcherFromEnvironment(repository conversation.Repository) (runnerapi.MessageDispatcher, error) {
	runnerURL := os.Getenv("RUNNER_AGENT_URL")
	namespace := os.Getenv("KUBERNETES_NAMESPACE")
	if runnerURL == "" && namespace == "" {
		return nil, nil
	}
	runStore, ok := repository.(runs.Store)
	if !ok {
		return nil, errors.New("runner agent requires a run-capable repository")
	}
	params := agent.ThreadStartParams{Model: os.Getenv("CODEX_MODEL"), CWD: os.Getenv("CODEX_CWD"), ApprovalPolicy: os.Getenv("CODEX_APPROVAL_POLICY")}
	var resolver agentclient.EndpointResolver
	if runnerURL != "" {
		resolver = agentclient.StaticEndpointResolver{URL: runnerURL}
	} else {
		resolver = kube.SessionEndpointResolver{Namespace: namespace}
	}
	factory := agentclient.NewRunClientFactoryWithToken(resolver, params, os.Getenv("RUNNER_AGENT_TOKEN"))
	return runs.NewMessageDispatcherWithFactory(runStore, factory), nil
}

type apiSessionActivator struct {
	activator session.Activator
	activity  session.ActivityRecorder
}

func (a apiSessionActivator) Activate(ctx context.Context, conversationID string) error {
	return a.ActivateConversation(ctx, conversationID, nil)
}

func (a apiSessionActivator) ActivateConversation(ctx context.Context, conversationID string, repository *conversation.RepositoryConfig) error {
	var sessionRepository *session.Repository
	if repository != nil {
		sessionRepository = &session.Repository{URL: repository.URL, Revision: repository.Revision}
	}
	return a.activator.Activate(ctx, session.Activation{ConversationID: conversationID, Repository: sessionRepository})
}

func (a apiSessionActivator) Touch(ctx context.Context, conversationID string, at time.Time) error {
	return a.activity.Touch(ctx, conversationID, at)
}

func sessionActivatorFromEnvironment() runnerapi.SessionActivator {
	namespace := os.Getenv("KUBERNETES_NAMESPACE")
	if namespace == "" {
		return nil
	}
	config, err := ctrl.GetConfig()
	if err != nil {
		log.Printf("Kubernetes session activation disabled: %v", err)
		return nil
	}
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		log.Printf("Kubernetes session activation disabled: %v", err)
		return nil
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		log.Printf("Kubernetes session activation disabled: %v", err)
		return nil
	}
	kubeClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		log.Printf("Kubernetes session activation disabled: %v", err)
		return nil
	}
	defaults := v1alpha1.CodexSessionSpec{SessionData: v1alpha1.SessionData{Size: "10Gi", Retain: true}, Execution: v1alpha1.Execution{ApprovalPolicy: os.Getenv("CODEX_APPROVAL_POLICY")}}
	if defaults.Execution.ApprovalPolicy == "" {
		defaults.Execution.ApprovalPolicy = "user"
	}
	concrete := kube.SessionActivator{Client: kubeClient, Namespace: namespace, Defaults: defaults}
	return apiSessionActivator{activator: concrete, activity: concrete}
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

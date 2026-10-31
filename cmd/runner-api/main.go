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
	apiHandler := runnerapi.NewHandlerWithDispatcher(repository, dispatcher)
	if eventStore, ok := repository.(runnerapi.RunEventStore); ok {
		apiHandler = runnerapi.NewHandlerWithDispatcherAndEvents(repository, dispatcher, eventStore, events.NewBroker())
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           runnerapi.NewRouter(apiHandler),
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

func messageDispatcherFromEnvironment(repository conversation.Repository) (runnerapi.MessageDispatcher, error) {
	agentURL := os.Getenv("RUNNER_AGENT_URL")
	if agentURL == "" {
		return nil, nil
	}
	runStore, ok := repository.(runs.Store)
	if !ok {
		return nil, errors.New("RUNNER_AGENT_URL requires a run-capable libSQL repository")
	}
	params := agent.ThreadStartParams{
		Model:          os.Getenv("CODEX_MODEL"),
		CWD:            os.Getenv("CODEX_CWD"),
		ApprovalPolicy: os.Getenv("CODEX_APPROVAL_POLICY"),
	}
	resolver := agentclient.StaticEndpointResolver{URL: agentURL}
	factory := agentclient.NewRunClientFactory(resolver, params)
	return runs.NewMessageDispatcherWithFactory(runStore, factory), nil
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

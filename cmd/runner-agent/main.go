package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/agent"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	command := os.Getenv("CODEX_APP_SERVER_COMMAND")
	if command == "" {
		command = "codex"
	}
	args := strings.Fields(os.Getenv("CODEX_APP_SERVER_ARGS"))
	if len(args) == 0 {
		args = []string{"app-server", "--listen", "stdio://"}
	}

	tracker := agent.NewRunTracker()
	process, err := agent.StartProcess(ctx, command, args, func(message agent.Message) {
		log.Printf("app-server event: run=%s method=%s", tracker.Current(), message.Method)
	})
	if err != nil {
		log.Fatal(err)
	}

	session := agent.NewSession(process.Client)
	if err := session.Initialize(ctx, agent.ClientInfo{
		Name:    "codex-runner-controller",
		Title:   "Codex Runner Controller",
		Version: "0.1.0",
	}); err != nil {
		log.Fatal(err)
	}

	address := os.Getenv("RUNNER_AGENT_ADDR")
	if address == "" {
		address = ":8081"
	}
	server := &http.Server{
		Addr:              address,
		Handler:           agent.NewHTTPHandlerWithTracker(session, tracker),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serverErr := make(chan error, 1)
	go func() {
		log.Printf("runner-agent listening on %s", address)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	processErr := make(chan error, 1)
	go func() { processErr <- process.Wait() }()
	select {
	case err := <-serverErr:
		_ = process.Close()
		log.Fatal(err)
	case err := <-processErr:
		if ctx.Err() == nil {
			log.Fatalf("app-server exited: %v", err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		_ = process.Close()
	}
}

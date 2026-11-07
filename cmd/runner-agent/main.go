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
	var runEventSink agent.RunEventSink
	if eventURL := os.Getenv("RUNNER_EVENT_URL"); eventURL != "" {
		runEventSink = agent.NewHTTPRunEventSink(eventURL)
	}
	process, err := agent.StartProcess(ctx, command, args, func(notification agent.AppServerNotification) {
		runID := tracker.Current()
		log.Printf("app-server notification: run=%s method=%s", runID, notification.Method)
		if runEventSink == nil || runID == "" {
			return
		}
		// Notification handling must not block the JSON-RPC reader. A short
		// timeout bounds delivery while the durable event store remains the
		// source of truth for replay.
		go func() {
			eventCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := runEventSink.Report(eventCtx, agent.AppServerRunEvent{
				RunID: runID, EventType: notification.Method, Payload: notification.Params,
			}); err != nil {
				log.Printf("forward App Server run event: %v", err)
			}
		}()
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

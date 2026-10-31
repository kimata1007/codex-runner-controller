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
	"github.com/kimata1007/codex-runner-controller/internal/auth"
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
	var runEventSender agent.RunEventSender
	if eventURL := os.Getenv("RUNNER_EVENT_URL"); eventURL != "" {
		runEventSender = agent.NewHTTPRunEventSender(eventURL).WithBearerToken(os.Getenv("RUNNER_API_TOKEN"))
	}
	approvals := agent.NewApprovalManager(runEventSender, tracker)
	process, err := agent.StartProcess(ctx, command, args, func(notification agent.AppServerNotification) {
		runID := tracker.Current()
		log.Printf("app-server notification: run=%s method=%s", runID, notification.Method)
		if runEventSender == nil || runID == "" {
			if terminalAppServerEvent(notification.Method) {
				tracker.Clear(runID)
			}
			return
		}
		// Notification handling must not block the JSON-RPC reader. A short
		// timeout bounds delivery while the durable event store remains the
		// source of truth for replay.
		go func() {
			eventCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := runEventSender.Send(eventCtx, agent.AppServerRunEvent{
				RunID: runID, EventType: notification.Method, Payload: notification.Params,
			}); err != nil {
				log.Printf("send App Server run event: %v", err)
			}
		}()
		if terminalAppServerEvent(notification.Method) {
			tracker.Clear(runID)
		}
	})
	if err != nil {
		log.Fatal(err)
	}
	process.Client.SetRequestHandler(approvals.Handle)
	if heartbeatURL := os.Getenv("RUNNER_HEARTBEAT_URL"); heartbeatURL != "" {
		heartbeat, heartbeatErr := agent.NewHTTPHeartbeatSinkWithToken(heartbeatURL, os.Getenv("CODEX_CONVERSATION_ID"), os.Getenv("RUNNER_AGENT_TOKEN"))
		if heartbeatErr != nil {
			log.Printf("configure heartbeat: %v", heartbeatErr)
		} else {
			go runHeartbeat(ctx, heartbeat, tracker)
		}
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
		Handler:           auth.BearerToken(agent.NewHTTPHandlerWithTrackerAndApprovals(session, tracker, approvals), os.Getenv("RUNNER_AGENT_TOKEN")),
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

func runHeartbeat(ctx context.Context, heartbeat *agent.HTTPHeartbeatSink, tracker *agent.RunTracker) {
	const interval = 30 * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	report := func() {
		if tracker.Current() == "" {
			return
		}
		heartbeatCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := heartbeat.Report(heartbeatCtx); err != nil && ctx.Err() == nil {
			log.Printf("report heartbeat: %v", err)
		}
	}
	report()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			report()
		}
	}
}

func terminalAppServerEvent(method string) bool {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "turn/completed", "turn/complete", "turn/finished", "turn/failed", "turn/error", "turn/interrupted", "turn/aborted":
		return true
	default:
		return false
	}
}

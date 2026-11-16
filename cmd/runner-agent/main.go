package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/agent"
	"github.com/kimata1007/codex-runner-controller/internal/auth"
	"github.com/kimata1007/codex-runner-controller/internal/logging"
)

func main() {
	slog.SetDefault(logging.New("runner-agent"))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	command := os.Getenv("CODEX_APP_SERVER_COMMAND")
	if command == "" {
		command = "codex"
	}
	args := strings.Fields(os.Getenv("CODEX_APP_SERVER_ARGS"))
	if len(args) == 0 {
		args = defaultAppServerArgs()
	}

	tracker := agent.NewRunTracker()
	var runEventSender agent.RunEventSender
	if eventURL := os.Getenv("RUNNER_EVENT_URL"); eventURL != "" {
		runEventSender = agent.NewHTTPRunEventSender(eventURL).WithBearerToken(os.Getenv("RUNNER_API_TOKEN"))
		runEventSender = agent.NewQueuedRunEventSender(ctx, runEventSender, func(err error) {
			slog.Error("failed to forward App Server run event", "operation", "event.forward", "error.type", logging.ErrorType(err))
		})
	}
	approvals := agent.NewApprovalManager(runEventSender, tracker)
	process, err := agent.StartProcess(ctx, command, args, func(notification agent.AppServerNotification) {
		runID := tracker.Current()
		slog.Debug("App Server notification received", "associated_with_run", runID != "", "method", notification.Method)
		if runEventSender == nil || runID == "" {
			if terminalAppServerEvent(notification.Method) {
				tracker.Clear(runID)
			}
			return
		}
		if err := runEventSender.Send(ctx, agent.AppServerRunEvent{
			RunID: runID, EventType: notification.Method, Payload: notification.Params,
		}); err != nil {
			slog.Error("failed to queue App Server run event", "operation", "event.queue", "run_id", runID, "event_type", notification.Method, "error.type", logging.ErrorType(err))
		}
		if terminalAppServerEvent(notification.Method) {
			tracker.Clear(runID)
		}
	})
	if err != nil {
		slog.Error("failed to start App Server", "operation", "app_server.start", "error.type", logging.ErrorType(err))
		os.Exit(1)
	}
	if heartbeatURL := os.Getenv("RUNNER_HEARTBEAT_URL"); heartbeatURL != "" {
		heartbeat, heartbeatErr := agent.NewHTTPHeartbeatSinkWithToken(heartbeatURL, os.Getenv("CODEX_CONVERSATION_ID"), os.Getenv("RUNNER_AGENT_TOKEN"))
		if heartbeatErr != nil {
			slog.Error("failed to configure heartbeat", "operation", "heartbeat.configure", "error.type", logging.ErrorType(heartbeatErr))
		} else {
			go runHeartbeat(ctx, heartbeat, tracker)
		}
	}

	session := agent.NewSession(process.Client)
	authRefresher := agent.ChatGPTAuthTokenRefresher{URL: os.Getenv("RUNNER_AUTH_REFRESH_URL"), Bearer: os.Getenv("RUNNER_AGENT_TOKEN")}
	session.SetRequestHandler(agent.ChainRequestHandlers(approvals.Handle, authRefresher.Handle))
	if err := session.Initialize(ctx, agent.ClientInfo{
		Name:    "codex-runner-controller",
		Title:   "Codex Runner Controller",
		Version: "0.1.0",
	}); err != nil {
		slog.Error("failed to initialize App Server session", "operation", "app_server.initialize", "error.type", logging.ErrorType(err))
		os.Exit(1)
	}

	address := os.Getenv("RUNNER_AGENT_ADDR")
	if address == "" {
		address = ":8081"
	}
	server := &http.Server{
		Addr:              address,
		Handler:           auth.BearerToken(agent.NewHTTPHandlerWithAuthBroker(session, tracker, approvals, os.Getenv("RUNNER_AUTH_BROKER") == "true"), os.Getenv("RUNNER_AGENT_TOKEN")),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serverErr := make(chan error, 1)
	go func() {
		slog.Info("HTTP server listening", "address", address)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	processErr := make(chan error, 1)
	go func() { processErr <- process.Wait() }()
	select {
	case err := <-serverErr:
		_ = process.Close()
		slog.Error("HTTP server failed", "operation", "http.server_start", "error.type", logging.ErrorType(err))
		os.Exit(1)
	case err := <-processErr:
		if ctx.Err() == nil {
			slog.Error("App Server exited unexpectedly", "operation", "app_server.exit", "error.type", logging.ErrorType(err))
			os.Exit(1)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		_ = process.Close()
	}
}

func defaultAppServerArgs() []string {
	args := []string{"app-server", "--listen", "stdio://"}
	if strings.TrimSpace(os.Getenv("ACCESS_TOKEN")) == "" {
		return args
	}
	return append(args,
		"-c", `model_provider="openai_chatgpt_plan"`,
		"-c", `model_providers.openai_chatgpt_plan.name="ChatGPT plan"`,
		"-c", `model_providers.openai_chatgpt_plan.base_url="https://api.openai.com/v1"`,
		"-c", `model_providers.openai_chatgpt_plan.env_key="ACCESS_TOKEN"`,
		"-c", `model_providers.openai_chatgpt_plan.wire_api="responses"`,
		"-c", `model_providers.openai_chatgpt_plan.requires_openai_auth=false`,
		"-c", `model_providers.openai_chatgpt_plan.supports_websockets=false`,
	)
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
			slog.Error("failed to report session heartbeat", "operation", "heartbeat.report", "error.type", logging.ErrorType(err))
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

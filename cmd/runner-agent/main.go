package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/agent"
	"github.com/kimata1007/codex-runner-controller/internal/auth"
	"github.com/kimata1007/codex-runner-controller/internal/logging"
	"github.com/kimata1007/codex-runner-controller/internal/sessioncontrol"
	"github.com/kimata1007/codex-runner-controller/internal/telemetry"
	"github.com/kimata1007/codex-runner-controller/internal/webmcp"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "-web-mcp" {
		if err := webmcp.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "-probe" {
		path := os.Args[2]
		if path != "/readyz" && path != "/healthz" {
			os.Exit(1)
		}
		client := http.Client{Timeout: time.Second}
		response, err := client.Get("http://127.0.0.1:8081" + path)
		if err != nil {
			os.Exit(1)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		return
	}
	slog.SetDefault(logging.New("runner-agent"))
	shutdown, err := telemetry.Start(context.Background(), "runner-agent")
	if err != nil {
		slog.Error("failed to initialize tracing", "operation", "startup.tracing", "error.type", logging.ErrorType(err))
		os.Exit(1)
	}
	defer telemetry.Shutdown(context.Background(), shutdown)
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
	isolated := os.Getenv("CODEX_ISOLATED_SESSION") == "true"
	var control *agent.SessionControl
	var runEventSender agent.RunEventSender
	if isolated {
		for _, name := range []string{"RUNNER_AGENT_TOKEN", "RUNNER_API_TOKEN", "RUNNER_EVENT_URL", "RUNNER_HEARTBEAT_URL", "RUNNER_AUTH_REFRESH_URL", "ACCESS_TOKEN", "GIT_AUTH_TOKEN"} {
			if os.Getenv(name) != "" {
				slog.Error("isolated session has a legacy credential or callback", "variable", name)
				os.Exit(1)
			}
		}
		control = agent.NewSessionControl(ctx, tracker)
		runEventSender = control
	} else if eventURL := os.Getenv("RUNNER_EVENT_URL"); eventURL != "" {
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
	if isolated {
		session.SetRequestHandler(approvals.Handle)
	} else {
		session.SetRequestHandler(agent.ChainRequestHandlers(approvals.Handle, authRefresher.Handle))
	}
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
	var handler http.Handler
	if isolated {
		mux := http.NewServeMux()
		mux.Handle(sessioncontrol.Path, control.Handler(session, approvals))
		health := func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
		mux.HandleFunc("GET /readyz", health)
		mux.HandleFunc("GET /healthz", health)
		handler = mux
	} else {
		handler = auth.BearerToken(agent.NewHTTPHandlerWithAuthBroker(session, tracker, approvals, os.Getenv("RUNNER_AUTH_BROKER") == "true"), os.Getenv("RUNNER_AGENT_TOKEN"))
	}
	server := &http.Server{
		Addr:              address,
		Handler:           telemetry.HTTPHandler(handler),
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
	// Kubernetes already isolates each conversation Pod. Do not invoke the Codex Linux sandbox.
	args := []string{"-c", `sandbox_mode="danger-full-access"`, "app-server", "--listen", "stdio://"}
	if os.Getenv("CODEX_ISOLATED_SESSION") == "true" {
		args = append([]string{
			"-c", `web_search="disabled"`,
			"-c", `model_provider="codex_session_proxy"`,
			"-c", `model_providers.codex_session_proxy.name="Codex Session Model Proxy"`,
			"-c", `model_providers.codex_session_proxy.base_url="http://198.18.0.13:8080/v1"`,
			"-c", `model_providers.codex_session_proxy.wire_api="responses"`,
			"-c", `model_providers.codex_session_proxy.requires_openai_auth=false`,
			"-c", `model_providers.codex_session_proxy.supports_websockets=false`,
		}, args...)
		if os.Getenv("CODEX_WEB_PROXY_ENABLED") == "true" {
			args = append([]string{
				"-c", `mcp_servers.codex_web_proxy.command="/runner-agent"`,
				"-c", `mcp_servers.codex_web_proxy.args=["-web-mcp"]`,
				"-c", `mcp_servers.codex_web_proxy.startup_timeout_sec=10`,
			}, args...)
		}
	}
	if endpoint := strings.TrimRight(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"), "/"); endpoint != "" {
		// Codex has its own OTLP trace exporter; its configuration is not
		// inherited from the Go OpenTelemetry SDK environment alone.
		// Avoid exporting logs, prompts, tool output and credentials.
		args = append([]string{"-c", "otel.trace_exporter={otlp-http={endpoint=" + strconv.Quote(endpoint+"/v1/traces") + ",protocol=\"binary\"}}", "-c", "otel.log_user_prompt=false", "-c", "otel.exporter=\"none\""}, args...)
	}
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

// Package telemetry adds opt-in, privacy-conscious OpenTelemetry tracing.
// Spans include operation names and redacted error descriptions, never raw
// request/response bodies, authentication headers, or prompts.
package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/logging"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const instrumentation = "github.com/kimata1007/codex-runner-controller"

// Start configures an OTLP/HTTP exporter only when OTEL_EXPORTER_OTLP_ENDPOINT
// is explicitly set. Without it, tracing remains no-op. OpenTelemetry's
// standard OTLP environment variables configure endpoint and TLS behavior.
func Start(ctx context.Context, service string) (func(context.Context) error, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" || os.Getenv("OTEL_SDK_DISABLED") == "true" {
		return func(context.Context) error { return nil }, nil
	}
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes("",
			attribute.String("service.name", service),
		)),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	slog.Info("OpenTelemetry tracing enabled", "service.name", service)
	return provider.Shutdown, nil
}

func Tracer() trace.Tracer { return otel.Tracer(instrumentation) }

// RecordError records the actual error after stripping common credentials and
// structured input fields. Callers must not pass request/response bodies.
func RecordError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	span := trace.SpanFromContext(ctx)
	message := logging.SanitizeErrorMessage(err.Error())
	span.SetStatus(codes.Error, message)
	kind := logging.ErrorType(err)
	span.SetAttributes(attribute.String("error.type", kind))
	span.AddEvent("exception", trace.WithAttributes(
		attribute.String("exception.message", message),
		attribute.String("exception.type", kind),
	))
}

// HTTPHandler instruments inbound HTTP traffic. Health checks are not traced.
// Route parameters and HTTP bodies are never stored as span attributes.
func HTTPHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := Tracer().Start(ctx, "http.server", trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("http.request.method", r.Method)))
		defer span.End()
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r.WithContext(ctx))
		code := recorder.status
		if code == 0 {
			code = http.StatusOK
		}
		span.SetAttributes(attribute.Int("http.response.status_code", code))
		if code >= 500 {
			span.SetStatus(codes.Error, "server error")
		}
	})
}

// HTTPTransport injects W3C traceparent into outbound service-to-service
// requests. Authorization and request contents are not recorded.
func HTTPTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return roundTripper{base: base}
}

type roundTripper struct{ base http.RoundTripper }

func (t roundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	attrs := []attribute.KeyValue{attribute.String("http.request.method", r.Method)}
	name := "http.client"
	if route := safeHTTPRoute(r.URL.Path); route != "" {
		attrs = append(attrs, attribute.String("http.route", route), attribute.String("url.path", logging.SanitizeHTTPPath(r.URL.Path)))
		name = r.Method + " " + route
	}
	ctx, span := Tracer().Start(r.Context(), name, trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...))
	defer span.End()
	out := r.Clone(ctx)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(out.Header))
	response, err := t.base.RoundTrip(out)
	if err != nil {
		RecordError(ctx, err)
		return nil, err
	}
	span.SetAttributes(attribute.Int("http.response.status_code", response.StatusCode))
	if response.StatusCode >= 400 {
		span.SetStatus(codes.Error, "upstream request failed")
	}
	return response, nil
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

func (w *statusRecorder) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Shutdown waits for batched traces to flush on graceful service termination.
func Shutdown(ctx context.Context, shutdown func(context.Context) error) {
	if shutdown == nil {
		return
	}
	flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := shutdown(flushCtx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("OpenTelemetry trace shutdown failed")
	}
}

// SetHTTPRoute attaches the matched framework route, not the raw URL.
// This prevents conversation identifiers and query parameters from leaking.
func SetHTTPRoute(ctx context.Context, method, route string) {
	if !strings.HasPrefix(route, "/") || len(route) > 160 {
		return
	}
	span := trace.SpanFromContext(ctx)
	span.SetName(method + " " + route)
	span.SetAttributes(attribute.String("http.route", route))
}

// safeHTTPRoute is an allowlist of known outbound endpoints. Unknown routes
// receive no path attribute rather than exposing their potentially sensitive IDs.
func safeHTTPRoute(path string) string {
	switch path {
	case "/v1/conversations", "/v1/auth/status", "/v1/auth/login", "/v1/auth",
		"/v1/codex/settings", "/v1/threads", "/v1/auth/tokens",
		"/v1/auth/device-login", "/v1/auth/account", "/v1/auth/credentials":
		return path
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case len(parts) == 3 && parts[0] == "v1" && parts[1] == "conversations":
		return "/v1/conversations/:id"
	case len(parts) == 4 && parts[0] == "v1" && parts[1] == "conversations" && parts[3] == "messages":
		return "/v1/conversations/:id/messages"
	case len(parts) == 4 && parts[0] == "v1" && parts[1] == "conversations" && parts[3] == "approvals":
		return "/v1/conversations/:id/approvals"
	case len(parts) == 4 && parts[0] == "v1" && parts[1] == "threads" && parts[3] == "resume":
		return "/v1/threads/:id/resume"
	case len(parts) == 4 && parts[0] == "v1" && parts[1] == "threads" && parts[3] == "turns":
		return "/v1/threads/:id/turns"
	case len(parts) == 3 && parts[0] == "v1" && parts[1] == "approvals":
		return "/v1/approvals/:id"
	case len(parts) == 4 && parts[0] == "v1" && parts[1] == "approvals":
		return "/v1/approvals/:runID/:requestID"
	case len(parts) == 4 && parts[0] == "v1" && parts[1] == "auth" && parts[2] == "device-login":
		return "/v1/auth/device-login/:loginID"
	case len(parts) == 4 && parts[0] == "v1" && parts[1] == "runs" && parts[3] == "events":
		return "/v1/runs/:runID/events"
	case len(parts) == 4 && parts[0] == "internal" && parts[1] == "runs" && parts[3] == "events":
		return "/internal/runs/:runID/events"
	case len(parts) == 4 && parts[0] == "internal" && parts[1] == "sessions" && parts[3] == "heartbeat":
		return "/internal/sessions/:conversationID/heartbeat"
	case len(parts) == 3 && parts[0] == "internal" && parts[1] == "auth" && parts[2] == "refresh":
		return "/internal/auth/refresh"
	default:
		return ""
	}
}

// Package telemetry adds opt-in, privacy-conscious OpenTelemetry tracing.
// Only span names, bounded status codes, and safe operation names are emitted;
// request/response bodies and authentication headers are never captured.
package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

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

// RecordError marks the current span as failed, without attaching arbitrary
// error messages that could include user prompts, tokens or API payloads.
func RecordError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	span := trace.SpanFromContext(ctx)
	span.SetStatus(codes.Error, "operation failed")
	span.SetAttributes(attribute.String("error.type", "operation_failed"))
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
	ctx, span := Tracer().Start(r.Context(), "http.client", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("http.request.method", r.Method)))
	defer span.End()
	out := r.Clone(ctx)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(out.Header))
	response, err := t.base.RoundTrip(out)
	if err != nil {
		RecordError(ctx, err)
		return nil, err
	}
	span.SetAttributes(attribute.Int("http.response.status_code", response.StatusCode))
	if response.StatusCode >= 500 {
		span.SetStatus(codes.Error, "upstream server error")
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

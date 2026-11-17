package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestHTTPHandlerAndTransportPropagateTraceContext(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	}()
	var incoming string
	server := httptest.NewServer(HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		incoming = r.Header.Get("traceparent")
		w.WriteHeader(http.StatusBadGateway)
	})))
	defer server.Close()

	ctx, parent := Tracer().Start(context.Background(), "test.parent")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := http.Client{Transport: HTTPTransport(nil)}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	parent.End()

	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if incoming == "" {
		t.Fatal("missing W3C traceparent header")
	}
	spans := exporter.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("spans = %d, want 3", len(spans))
	}
	var serverFound, clientFound bool
	for _, span := range spans {
		switch span.SpanKind {
		case trace.SpanKindServer:
			serverFound = true
		case trace.SpanKindClient:
			clientFound = true
		}
	}
	if !serverFound || !clientFound {
		t.Fatalf("missing client/server spans: %#v", spans)
	}
}

func TestHTTPHandlerSkipsHealthChecks(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	old := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	defer func() { otel.SetTracerProvider(old); _ = provider.Shutdown(context.Background()) }()
	handler := HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for _, path := range []string{"/healthz", "/readyz"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	if len(exporter.GetSpans()) != 0 {
		t.Fatal("health checks should not create spans")
	}
}

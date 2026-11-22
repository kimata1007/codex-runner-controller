package telemetry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kimata1007/codex-runner-controller/internal/logging"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestSafeHTTPRouteAvoidsIdentifiersAndUnknownPaths(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/v1/threads/secret-thread/turns", "/v1/threads/:id/turns"},
		{"/v1/threads/secret-thread/resume", "/v1/threads/:id/resume"},
		{"/v1/approvals/secret-id", "/v1/approvals/:id"},
		{"/v1/approvals/run-secret/request-secret", "/v1/approvals/:runID/:requestID"},
		{"/internal/runs/secret-run/events", "/internal/runs/:runID/events"},
		{"/internal/sessions/secret-session/heartbeat", "/internal/sessions/:conversationID/heartbeat"},
		{"/unknown/secret-token", ""},
	}
	for _, tt := range tests {
		if got := safeHTTPRoute(tt.path); got != tt.want {
			t.Errorf("safeHTTPRoute(%q) = %q, want %q", tt.path, got, tt.want)
		}
		if strings.Contains(safeHTTPRoute(tt.path), "secret") {
			t.Fatalf("raw identifier leaked: %q", tt.path)
		}
	}
}

func TestHTTPServerSpanUsesMatchedRouteNotUserID(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	defer func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	}()
	router := echo.New()
	router.Use(logging.EchoRequestLogger(logging.New("test")))
	router.POST("/v1/threads/:id/turns", func(c echo.Context) error {
		return c.NoContent(http.StatusAccepted)
	})
	handler := HTTPHandler(router)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/threads/private-thread/turns", nil))
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d", response.Code)
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Name != "POST /v1/threads/:id/turns" {
		t.Fatalf("unexpected spans: %+v", spans)
	}
	for _, attr := range spans[0].Attributes {
		if attr.Key == attribute.Key("http.route") && attr.Value.AsString() != "/v1/threads/:id/turns" {
			t.Fatalf("route = %s", attr.Value.AsString())
		}
		if attr.Key == attribute.Key("url.path") && attr.Value.AsString() != "/v1/threads/private-thread/turns" {
			t.Fatalf("full path (with ID) missing: %v", attr)
		}
	}
}

func TestRecordErrorCreatesSafeErrorEvent(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	defer func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	}()
	ctx, span := Tracer().Start(context.Background(), "test.operation")
	RecordError(ctx, errors.New("forbidden secret_access_token=private-value"))
	span.End()
	spans := exporter.GetSpans()
	if len(spans) != 1 || len(spans[0].Events) != 1 {
		t.Fatalf("unexpected events: %+v", spans)
	}
	for _, event := range spans[0].Events {
		for _, attr := range event.Attributes {
			if strings.Contains(attr.Value.Emit(), "private-value") {
				t.Fatalf("secret leaked in event: %+v", attr)
			}
			if attr.Key == "exception.message" && !strings.Contains(attr.Value.AsString(), "forbidden") {
				t.Fatalf("useful error message was lost: %q", attr.Value.AsString())
			}
		}
	}
}

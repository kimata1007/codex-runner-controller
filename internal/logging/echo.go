package logging

import (
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// EchoRequestLogger logs the method, matched route, response status, and
// duration for each request. Health checks are omitted to keep probe traffic
// from obscuring application logs.
func EchoRequestLogger(logger *slog.Logger) echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogMethod:    true,
		LogRoutePath: true,
		LogStatus:    true,
		LogLatency:   true,
		LogError:     true,
		HandleError:  true,
		Skipper: func(c echo.Context) bool {
			return c.Path() == "/healthz" || c.Path() == "/readyz"
		},
		LogValuesFunc: func(c echo.Context, values middleware.RequestLoggerValues) error {
			attributes := []any{
				"http.request.method", values.Method,
				"http.route", values.RoutePath,
				"http.response.status_code", values.Status,
				"duration_ms", values.Latency.Milliseconds(),
			}
			// Echo's matched route is a template, not a raw URI containing IDs.
			if route := c.Path(); route != "" {
				span := trace.SpanFromContext(c.Request().Context())
				span.SetName(values.Method + " " + route)
				span.SetAttributes(attribute.String("http.route", route), attribute.String("url.path", SanitizeHTTPPath(c.Request().URL.Path)))
			}
			spanCtx := trace.SpanContextFromContext(c.Request().Context())
			if spanCtx.IsValid() {
				attributes = append(attributes, "trace_id", spanCtx.TraceID().String(), "span_id", spanCtx.SpanID().String())
			}
			if values.Error != nil {
				attributes = append(attributes, "error.type", ErrorType(values.Error))
			}
			logger.InfoContext(c.Request().Context(), "HTTP request completed", attributes...)
			return nil
		},
	})
}

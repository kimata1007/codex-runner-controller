package logging

import (
	"log/slog"

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
			if values.Error != nil {
				attributes = append(attributes, "error", values.Error)
			}
			logger.InfoContext(c.Request().Context(), "HTTP request completed", attributes...)
			return nil
		},
	})
}

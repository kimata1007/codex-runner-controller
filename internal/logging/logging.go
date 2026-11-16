package logging

import (
	"log/slog"
	"os"
)

// New returns a human-readable structured logger for a service.
func New(service string) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, nil)).With("service.name", service)
}

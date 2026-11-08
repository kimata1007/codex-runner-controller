package events

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/kimata1007/codex-runner-controller/internal/runs"
	"github.com/labstack/echo/v4"
)

type Store interface {
	ListEvents(context.Context, string, int64) ([]runs.Event, error)
}

type Subscriber interface {
	Subscribe(string) (<-chan runs.Event, func())
}

type SSEHandler struct {
	store  Store
	broker Subscriber
}

func NewSSEHandler(store Store, broker Subscriber) echo.HandlerFunc {
	handler := &SSEHandler{store: store, broker: broker}
	return handler.events
}

func (h *SSEHandler) events(c echo.Context) error {
	r := c.Request()
	response := c.Response()
	w := response
	runID := c.Param("runID")
	after, err := parseLastEventID(r)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if h.store == nil || h.broker == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "event stream is not configured")
	}

	channel, cancel := h.broker.Subscribe(runID)
	defer cancel()
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache")
	response.Header().Set("Connection", "keep-alive")
	flusher, ok := response.Writer.(http.Flusher)
	if !ok {
		return echo.NewHTTPError(http.StatusInternalServerError, "streaming is not supported")
	}

	if err := h.writeStored(r.Context(), w, flusher, runID, &after); err != nil {
		return err
	}
	for {
		select {
		case <-r.Context().Done():
			return nil
		case event, ok := <-channel:
			if !ok {
				return nil
			}
			if event.Sequence <= after {
				continue
			}
			if event.Sequence > after+1 {
				if err := h.writeStored(r.Context(), w, flusher, runID, &after); err != nil {
					return err
				}
				if event.Sequence <= after {
					continue
				}
			}
			if err := writeEvent(w, flusher, event); err != nil {
				return err
			}
			after = event.Sequence
		}
	}
}

func (h *SSEHandler) writeStored(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, runID string, after *int64) error {
	events, err := h.store.ListEvents(ctx, runID, *after)
	if err != nil {
		return err
	}
	for _, event := range events {
		if event.Sequence <= *after {
			continue
		}
		if err := writeEvent(w, flusher, event); err != nil {
			return err
		}
		*after = event.Sequence
	}
	return nil
}

func writeEvent(w http.ResponseWriter, flusher http.Flusher, event runs.Event) error {
	if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, event.EventType, event.Payload); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func parseLastEventID(r *http.Request) (int64, error) {
	value := r.Header.Get("Last-Event-ID")
	if value == "" {
		value = r.URL.Query().Get("after")
	}
	if value == "" {
		return 0, nil
	}
	sequence, err := strconv.ParseInt(value, 10, 64)
	if err != nil || sequence < 0 {
		return 0, fmt.Errorf("invalid event sequence %q", value)
	}
	return sequence, nil
}

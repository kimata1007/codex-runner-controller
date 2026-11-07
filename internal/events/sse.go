package events

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/kimata1007/codex-runner-controller/internal/runs"
)

type Store interface {
	ListEvents(context.Context, string, int64) ([]runs.Event, error)
}

type SSEHandler struct {
	store  Store
	broker *Broker
}

func NewSSEHandler(store Store, broker *Broker) http.Handler {
	handler := &SSEHandler{store: store, broker: broker}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/runs/", handler.events)
	return mux
}

func (h *SSEHandler) events(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/runs/"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "events" {
		http.Error(w, "route not found", http.StatusNotFound)
		return
	}
	runID := parts[0]
	after, err := parseLastEventID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if h.store == nil || h.broker == nil {
		http.Error(w, "event stream is not configured", http.StatusServiceUnavailable)
		return
	}

	channel, cancel := h.broker.Subscribe(runID)
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}

	if err := h.writeStored(r.Context(), w, flusher, runID, &after); err != nil {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-channel:
			if !ok {
				return
			}
			if event.Sequence <= after {
				continue
			}
			if event.Sequence > after+1 {
				if err := h.writeStored(r.Context(), w, flusher, runID, &after); err != nil {
					return
				}
				if event.Sequence <= after {
					continue
				}
			}
			if err := writeEvent(w, flusher, event); err != nil {
				return
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

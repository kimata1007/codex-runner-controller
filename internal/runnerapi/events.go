package runnerapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/kimata1007/codex-runner-controller/internal/runs"
	"github.com/labstack/echo/v4"
)

// RunEventStore persists runner-agent events before they are sent to live
// subscribers, so clients can replay every published event after reconnecting.
type RunEventStore interface {
	AppendEvent(context.Context, string, string, string) (runs.Event, error)
}

// RunEventPublisher sends a persisted event to currently connected clients.
type RunEventPublisher interface {
	Publish(runs.Event)
}

type runEventRequest struct {
	EventType string          `json:"eventType"`
	Payload   json.RawMessage `json:"payload"`
}

func (h *Handler) receiveRunEvent(c echo.Context) error {
	if h.eventStore == nil || h.publisher == nil {
		return writeError(c, http.StatusServiceUnavailable, "event receiver is not configured")
	}

	var request runEventRequest
	if err := decodeJSON(c, &request); err != nil {
		return err
	}
	request.EventType = strings.TrimSpace(request.EventType)
	if request.EventType == "" {
		return writeError(c, http.StatusBadRequest, "eventType is required")
	}
	if len(request.Payload) == 0 {
		request.Payload = json.RawMessage(`{}`)
	}
	if !json.Valid(request.Payload) {
		return writeError(c, http.StatusBadRequest, "payload must be valid JSON")
	}

	event, err := h.eventStore.AppendEvent(c.Request().Context(), c.Param("runID"), request.EventType, string(request.Payload))
	if err != nil {
		return writeError(c, http.StatusBadGateway, "failed to persist event")
	}
	if h.eventProjector != nil {
		if err := h.eventProjector.ApplyAppServerEvent(c.Request().Context(), event.RunID, event.EventType, event.Payload); err != nil {
			return writeError(c, http.StatusBadGateway, "failed to update run status")
		}
	}
	h.publisher.Publish(event)
	return writeJSON(c, http.StatusAccepted, event)
}

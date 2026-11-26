package runnerapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/conversation"
	"github.com/kimata1007/codex-runner-controller/internal/runs"
	"github.com/kimata1007/codex-runner-controller/internal/sessioncontrol"
	"github.com/labstack/echo/v4"
)

type ControlStore interface {
	BindControlRun(context.Context, string, string, string) error
	AuthorizeControlRun(context.Context, string, string, string) error
	ControlTurnID(context.Context, string) (string, error)
	AppendControlEvent(context.Context, string, string, string, int64, string, string) (runs.Event, error)
}

// UseIsolatedControl sets the control-backed dispatcher before starting the API.
func (h *Handler) UseIsolatedControl(dispatcher *runs.MessageDispatcher) {
	h.dispatcher, h.approvalResponder, h.isolatedControl = dispatcher, dispatcher, true
}

func (h *Handler) BindControlRun(ctx context.Context, conversationID, runID, podUID string) error {
	store, ok := h.repository.(ControlStore)
	if !ok {
		return errors.New("session control store is unavailable")
	}
	return store.BindControlRun(ctx, conversationID, runID, podUID)
}

func (h *Handler) AuthorizeControlRun(ctx context.Context, conversationID, runID, podUID string) error {
	store, ok := h.repository.(ControlStore)
	if !ok {
		return errors.New("session control store is unavailable")
	}
	return store.AuthorizeControlRun(ctx, conversationID, runID, podUID)
}

func (h *Handler) interruptRun(c echo.Context) error {
	store, configured := h.repository.(ControlStore)
	dispatcher, supported := h.dispatcher.(*runs.MessageDispatcher)
	if !configured || !supported {
		return writeError(c, http.StatusServiceUnavailable, "session control is unavailable")
	}
	runID, ctx := c.Param("runID"), c.Request().Context()
	turnID, err := store.ControlTurnID(ctx, runID)
	if err != nil {
		return writeOperationalError(c, http.StatusConflict, "active turn is unavailable", "turn_interrupt", err)
	}
	if err := dispatcher.Interrupt(ctx, runID, turnID); err != nil {
		return writeOperationalError(c, http.StatusBadGateway, "failed to interrupt the active turn", "turn_interrupt", err)
	}
	return writeJSON(c, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (h *Handler) ControlEvent(ctx context.Context, conversationID, podUID string, frame sessioncontrol.Frame) (bool, error) {
	store, ok := h.repository.(ControlStore)
	if !ok || h.eventProjector == nil || h.eventHub == nil || frame.Kind != sessioncontrol.Event || frame.Sequence > math.MaxInt64 {
		return false, errors.New("invalid control event or unavailable event store")
	}
	var payload bytes.Buffer
	if err := json.Compact(&payload, frame.Payload); err != nil {
		return false, err
	}
	event, err := store.AppendControlEvent(ctx, conversationID, podUID, frame.ID, int64(frame.Sequence), frame.Method, payload.String())
	if err != nil {
		return false, err
	}
	if err := h.eventProjector.ApplyAppServerEvent(ctx, event.RunID, event.EventType, event.Payload); err != nil {
		return false, err
	}
	h.projectSessionPhase(ctx, event.RunID, event.EventType, event.Payload)
	h.eventHub.Publish(event)
	status, _ := runs.StatusForEvent(event.EventType, event.Payload)
	return status == runs.StatusCompleted || status == runs.StatusFailed || status == runs.StatusInterrupted, nil
}

func (h *Handler) ControlHeartbeat(ctx context.Context, conversationID string) {
	at := time.Now().UTC()
	if h.activity != nil {
		_ = h.activity.Touch(ctx, conversationID, at)
	}
	if recorder, ok := h.repository.(conversation.ActivityRecorder); ok {
		_ = recorder.TouchConversation(ctx, conversationID, at)
	}
}

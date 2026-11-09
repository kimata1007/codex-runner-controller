package runnerapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/conversation"
	"github.com/kimata1007/codex-runner-controller/internal/events"
	"github.com/kimata1007/codex-runner-controller/internal/runs"
	"github.com/kimata1007/codex-runner-controller/internal/session"
	"github.com/labstack/echo/v4"
)

const maxRequestBodyBytes = 1 << 20

type Handler struct {
	repository conversation.Repository
	dispatcher MessageDispatcher
	activator  session.Activator
	eventStore RunEventStore
	eventHub   RunEventHub
}

// HandlerDependencies groups the optional collaborators used by the HTTP API.
// Keeping the constructor stable avoids encoding every new dependency in its name.
type HandlerDependencies struct {
	Repository conversation.Repository
	Dispatcher MessageDispatcher
	Activator  session.Activator
	EventStore RunEventStore
	EventHub   RunEventHub
}

func NewHandler(dependencies HandlerDependencies) *Handler {
	return &Handler{
		repository: dependencies.Repository,
		dispatcher: dependencies.Dispatcher,
		activator:  dependencies.Activator,
		eventStore: dependencies.EventStore,
		eventHub:   dependencies.EventHub,
	}
}

type MessageDispatcher interface {
	Dispatch(context.Context, string, string, string, string) (runs.Run, error)
}

func NewRouter(handler *Handler) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.GET("/healthz", handler.checkHealth)
	e.GET("/readyz", handler.checkHealth)
	e.POST("/v1/conversations", handler.createConversation)
	e.GET("/v1/conversations/:id", handler.getConversation)
	e.GET("/v1/conversations/:id/messages", handler.getMessages)
	e.POST("/v1/conversations/:id/messages", handler.createMessage)
	if handler.eventStore != nil && handler.eventHub != nil {
		e.POST("/internal/runs/:runID/events", handler.receiveAppServerRunEvent)
		e.GET("/v1/runs/:runID/events", events.NewSSEHandler(handler.eventStore, handler.eventHub))
	}
	return e
}

// RunEventStore persists App Server notifications that runner-agent associates
// with a run. The stored runs.Event can then be replayed after reconnection.
type RunEventStore interface {
	AppendEvent(context.Context, string, string, string) (runs.Event, error)
	events.Store
}

// RunEventHub sends persisted events to connected stream subscribers.
type RunEventHub interface {
	Publish(runs.Event)
	Subscribe(string) (<-chan runs.Event, func())
}

// appServerRunEventRequest is the HTTP representation of an AppServerRunEvent:
// eventType contains the JSON-RPC notification method, and payload its params.
type appServerRunEventRequest struct {
	EventType string          `json:"eventType"`
	Payload   json.RawMessage `json:"payload"`
}

func (h *Handler) receiveAppServerRunEvent(c echo.Context) error {
	if h.eventStore == nil || h.eventHub == nil {
		return writeError(c, http.StatusServiceUnavailable, "event receiver is not configured")
	}

	var request appServerRunEventRequest
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
	h.eventHub.Publish(event)
	return writeJSON(c, http.StatusAccepted, event)
}

func (h *Handler) checkHealth(c echo.Context) error {
	return writeJSON(c, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) createConversation(c echo.Context) error {
	value, err := h.repository.CreateConversation(c.Request().Context())
	if err != nil {
		return writeRepositoryError(c, err)
	}
	return writeJSON(c, http.StatusCreated, conversationResponseFrom(value))
}

func (h *Handler) getConversation(c echo.Context) error {
	value, err := h.repository.GetConversation(c.Request().Context(), c.Param("id"))
	if err != nil {
		return writeRepositoryError(c, err)
	}
	return writeJSON(c, http.StatusOK, conversationResponseFrom(value))
}

func (h *Handler) getMessages(c echo.Context) error {
	messages, err := h.repository.ListMessages(c.Request().Context(), c.Param("id"))
	if err != nil {
		return writeRepositoryError(c, err)
	}

	response := make([]messageResponse, 0, len(messages))
	for _, message := range messages {
		response = append(response, messageResponseFrom(message))
	}
	return writeJSON(c, http.StatusOK, map[string]any{"messages": response})
}

func (h *Handler) createMessage(c echo.Context) error {
	var request createMessageRequest
	if err := decodeJSON(c, &request); err != nil {
		return err
	}
	request.Content = strings.TrimSpace(request.Content)
	if request.Content == "" {
		return writeError(c, http.StatusBadRequest, "content is required")
	}
	conversationID := c.Param("id")
	if h.activator != nil {
		if _, err := h.repository.GetConversation(c.Request().Context(), conversationID); err != nil {
			return writeRepositoryError(c, err)
		}
		isDuplicate := false
		if request.ClientMessageID != "" {
			messages, err := h.repository.ListMessages(c.Request().Context(), conversationID)
			if err != nil {
				return writeRepositoryError(c, err)
			}
			for _, existing := range messages {
				if existing.ClientMessageID == request.ClientMessageID {
					isDuplicate = true
					break
				}
			}
		}
		if !isDuplicate {
			if err := h.activator.Activate(c.Request().Context(), session.Activation{ConversationID: conversationID}); err != nil {
				return writeError(c, http.StatusServiceUnavailable, "failed to activate session")
			}
		}
	}

	message, created, err := h.repository.CreateMessage(c.Request().Context(), conversationID, conversation.NewMessage{
		Content:         request.Content,
		ClientMessageID: request.ClientMessageID,
	})
	if err != nil {
		return writeRepositoryError(c, err)
	}
	if created && h.dispatcher != nil {
		run, err := h.dispatcher.Dispatch(c.Request().Context(), message.ConversationID, message.ID, request.ThreadID, message.Content)
		if err != nil {
			return writeError(c, http.StatusBadGateway, "failed to start run")
		}
		return writeJSON(c, http.StatusAccepted, map[string]any{
			"message": messageResponseFrom(message),
			"run":     run,
		})
	}
	status := http.StatusOK
	if created {
		status = http.StatusAccepted
	}
	return writeJSON(c, status, messageResponseFrom(message))
}

type createMessageRequest struct {
	Content         string `json:"content"`
	ClientMessageID string `json:"clientMessageId"`
	ThreadID        string `json:"threadId,omitempty"`
}

type conversationResponse struct {
	ID        string            `json:"id"`
	Status    string            `json:"status"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
	Messages  []messageResponse `json:"messages,omitempty"`
}

type messageResponse struct {
	ID              string    `json:"id"`
	ConversationID  string    `json:"conversationId"`
	Role            string    `json:"role"`
	Content         string    `json:"content"`
	ClientMessageID string    `json:"clientMessageId,omitempty"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"createdAt"`
}

func conversationResponseFrom(value conversation.Conversation) conversationResponse {
	response := conversationResponse{
		ID:        value.ID,
		Status:    value.Status,
		CreatedAt: value.CreatedAt,
		UpdatedAt: value.UpdatedAt,
	}
	if len(value.Messages) > 0 {
		response.Messages = make([]messageResponse, 0, len(value.Messages))
		for _, message := range value.Messages {
			response.Messages = append(response.Messages, messageResponseFrom(message))
		}
	}
	return response
}

func messageResponseFrom(value conversation.Message) messageResponse {
	return messageResponse{
		ID:              value.ID,
		ConversationID:  value.ConversationID,
		Role:            value.Role,
		Content:         value.Content,
		ClientMessageID: value.ClientMessageID,
		Status:          value.Status,
		CreatedAt:       value.CreatedAt,
	}
}

func decodeJSON(c echo.Context, destination any) error {
	r := c.Request()
	r.Body = http.MaxBytesReader(c.Response().Writer, r.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		if err == io.EOF {
			return writeError(c, http.StatusBadRequest, "request body is required")
		}
		return writeError(c, http.StatusBadRequest, "invalid JSON request")
	}
	return nil
}

func writeJSON(c echo.Context, status int, value any) error {
	return c.JSON(status, value)
}

func writeError(c echo.Context, status int, message string) error {
	return writeJSON(c, status, map[string]string{"error": message})
}

func writeRepositoryError(c echo.Context, err error) error {
	if errors.Is(err, conversation.ErrNotFound) {
		return writeError(c, http.StatusNotFound, "conversation not found")
	}
	return writeError(c, http.StatusInternalServerError, "internal server error")
}

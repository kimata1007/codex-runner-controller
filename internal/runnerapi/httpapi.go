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
	"github.com/kimata1007/codex-runner-controller/internal/runs"
	"github.com/labstack/echo/v4"
)

const maxRequestBodyBytes = 1 << 20

type Handler struct {
	repository conversation.Repository
	dispatcher MessageDispatcher
	eventStore RunEventStore
	publisher  RunEventPublisher
}

func NewHandler(repository conversation.Repository) *Handler {
	return NewHandlerWithDispatcher(repository, nil)
}

type MessageDispatcher interface {
	Dispatch(context.Context, string, string, string, string) (runs.Run, error)
}

func NewHandlerWithDispatcher(repository conversation.Repository, dispatcher MessageDispatcher) *Handler {
	return &Handler{repository: repository, dispatcher: dispatcher}
}

func NewHandlerWithDispatcherAndEvents(repository conversation.Repository, dispatcher MessageDispatcher, eventStore RunEventStore, publisher RunEventPublisher) *Handler {
	return &Handler{repository: repository, dispatcher: dispatcher, eventStore: eventStore, publisher: publisher}
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
	if handler.eventStore != nil && handler.publisher != nil {
		e.POST("/internal/runs/:runID/events", handler.receiveRunEvent)
	}
	return e
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

	message, created, err := h.repository.CreateMessage(c.Request().Context(), c.Param("id"), conversation.NewMessage{
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

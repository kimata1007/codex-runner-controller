package runnerapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/agent"
	"github.com/kimata1007/codex-runner-controller/internal/authbroker"
	"github.com/kimata1007/codex-runner-controller/internal/conversation"
	"github.com/kimata1007/codex-runner-controller/internal/events"
	"github.com/kimata1007/codex-runner-controller/internal/logging"
	"github.com/kimata1007/codex-runner-controller/internal/runs"
	"github.com/kimata1007/codex-runner-controller/internal/session"
	"github.com/kimata1007/codex-runner-controller/internal/telemetry"
	"github.com/labstack/echo/v4"
)

const maxRequestBodyBytes = 1 << 20

type Handler struct {
	repository        conversation.Repository
	statusStore       MessageStatusStore
	queueStore        QueuedMessageStore
	dispatcher        MessageDispatcher
	approvalResponder ApprovalResponder
	activator         session.Activator
	activity          session.ActivityRecorder
	phase             SessionPhaseRecorder
	eventStore        RunEventStore
	eventProjector    RunEventProjector
	eventHub          RunEventHub
	authBroker        ChatGPTAuthBroker
	isolatedControl   bool
}

// HandlerDependencies groups the optional collaborators used by the HTTP API.
// Keeping the constructor stable avoids encoding every new dependency in its name.
type HandlerDependencies struct {
	Repository conversation.Repository
	Dispatcher MessageDispatcher
	Activator  session.Activator
	EventStore RunEventStore
	EventHub   RunEventHub
	AuthBroker ChatGPTAuthBroker
}

type ChatGPTAuthBroker interface {
	Status(context.Context) (map[string]any, error)
	BeginLogin(context.Context) (authbroker.DeviceLogin, error)
	CancelLogin(string)
	GetChatGPTAuthTokens(context.Context) (agent.ChatGPTAuthTokens, error)
	RefreshChatGPTAuthTokens(context.Context, string) (agent.ChatGPTAuthTokens, error)
	Logout(context.Context) error
}

func NewHandler(dependencies HandlerDependencies) *Handler {
	statusStore, _ := dependencies.Repository.(MessageStatusStore)
	queueStore, _ := dependencies.Repository.(QueuedMessageStore)
	eventProjector, _ := dependencies.Repository.(RunEventProjector)
	approvalResponder, _ := dependencies.Dispatcher.(ApprovalResponder)
	activity, _ := dependencies.Activator.(session.ActivityRecorder)
	phase, _ := dependencies.Activator.(SessionPhaseRecorder)
	return &Handler{
		repository:        dependencies.Repository,
		statusStore:       statusStore,
		queueStore:        queueStore,
		dispatcher:        dependencies.Dispatcher,
		approvalResponder: approvalResponder,
		activator:         dependencies.Activator,
		activity:          activity,
		phase:             phase,
		eventStore:        dependencies.EventStore,
		eventProjector:    eventProjector,
		eventHub:          dependencies.EventHub,
		authBroker:        dependencies.AuthBroker,
	}
}

type MessageDispatcher interface {
	Dispatch(context.Context, string, string, string, string) (runs.Run, error)
}

type ApprovalResponder interface {
	RespondApproval(context.Context, string, string, any) error
}

// MessageStatusStore atomically claims queued messages and tracks dispatch state.
type MessageStatusStore interface {
	ClaimQueuedMessage(context.Context, string) (bool, error)
	SetMessageStatus(context.Context, string, string) error
}

// QueuedMessageStore provides durable, per-conversation FIFO dispatch claims.
type QueuedMessageStore interface {
	ListQueuedConversationIDs(context.Context) ([]string, error)
	ClaimNextQueuedMessage(context.Context, string) (conversation.Message, bool, error)
}

type SessionPhaseRecorder interface {
	SetPhase(context.Context, string, string) error
}
type RunEventProjector interface {
	ApplyAppServerEvent(context.Context, string, string, string) error
}

type ActiveRunLister interface {
	ListActiveRuns(context.Context, string) ([]runs.ActiveRun, error)
}

type ApprovalLister interface {
	ListPendingApprovals(context.Context, string) ([]runs.Approval, error)
}

func NewRouter(handler *Handler) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.Use(logging.EchoRequestLogger(slog.Default()))
	e.GET("/healthz", handler.checkHealth)
	e.GET("/readyz", handler.checkHealth)
	e.GET("/v1/conversations", handler.listConversations)
	if handler.authBroker != nil {
		e.GET("/v1/auth/status", handler.getGlobalAuthStatus)
		e.POST("/v1/auth/login", handler.beginGlobalAuth)
		e.DELETE("/v1/auth/login/:attemptID", handler.cancelGlobalAuth)
		e.DELETE("/v1/auth", handler.logoutGlobalAuth)
		e.POST("/internal/auth/refresh", handler.refreshGlobalAuth)
	}
	e.POST("/v1/conversations", handler.createConversation)
	e.GET("/v1/conversations/:id", handler.getConversation)
	e.DELETE("/v1/conversations/:id", handler.archiveConversation)
	e.GET("/v1/conversations/:id/messages", handler.getMessages)
	if _, ok := handler.repository.(ActiveRunLister); ok {
		e.GET("/v1/conversations/:id/runs/active", handler.listActiveRuns)
	}
	e.POST("/v1/conversations/:id/messages", handler.createMessage)
	if handler.approvalResponder != nil {
		e.POST("/v1/approvals/:runID/:requestID", handler.respondApproval)
	}
	if _, ok := handler.repository.(ApprovalLister); ok {
		e.GET("/v1/conversations/:id/approvals", handler.listPendingApprovals)
	}
	if handler.eventStore != nil && handler.eventHub != nil {
		if !handler.isolatedControl {
			e.POST("/internal/runs/:runID/events", handler.receiveAppServerRunEvent)
		}
		e.GET("/v1/runs/:runID/events", events.NewSSEHandler(handler.eventStore, handler.eventHub))
	}
	if handler.activity != nil && !handler.isolatedControl {
		e.POST("/internal/sessions/:conversationID/heartbeat", handler.receiveHeartbeat)
	}
	return e
}

func (h *Handler) getGlobalAuthStatus(c echo.Context) error {
	status, err := h.authBroker.Status(c.Request().Context())
	if err != nil {
		return writeOperationalError(c, http.StatusServiceUnavailable, "ChatGPT authentication status is unavailable", "auth_status", err)
	}
	return writeJSON(c, http.StatusOK, status)
}

func (h *Handler) beginGlobalAuth(c echo.Context) error {
	login, err := h.authBroker.BeginLogin(c.Request().Context())
	if err != nil {
		return writeOperationalError(c, http.StatusServiceUnavailable, "could not begin ChatGPT sign-in", "auth_login", err)
	}
	return writeJSON(c, http.StatusCreated, login)
}

func (h *Handler) cancelGlobalAuth(c echo.Context) error {
	h.authBroker.CancelLogin(c.Param("attemptID"))
	return writeJSON(c, http.StatusAccepted, map[string]string{"status": "cancelled"})
}

func (h *Handler) logoutGlobalAuth(c echo.Context) error {
	if err := h.authBroker.Logout(c.Request().Context()); err != nil {
		return writeOperationalError(c, http.StatusServiceUnavailable, "could not disconnect ChatGPT", "auth_logout", err)
	}
	return writeJSON(c, http.StatusOK, map[string]string{"status": "disconnected"})
}

func (h *Handler) refreshGlobalAuth(c echo.Context) error {
	var request struct {
		PreviousAccountID string `json:"previousAccountId"`
	}
	if err := decodeJSON(c, &request); err != nil {
		return err
	}
	tokens, err := h.authBroker.RefreshChatGPTAuthTokens(c.Request().Context(), request.PreviousAccountID)
	if err != nil {
		return writeOperationalError(c, http.StatusUnauthorized, "ChatGPT credentials need to be reconnected", "auth_refresh", err)
	}
	return writeJSON(c, http.StatusOK, tokens)
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
		return writeOperationalError(c, http.StatusBadGateway, "failed to persist event", "event_persist", err)
	}
	if h.eventProjector != nil {
		if err := h.eventProjector.ApplyAppServerEvent(c.Request().Context(), event.RunID, event.EventType, event.Payload); err != nil {
			return writeOperationalError(c, http.StatusBadGateway, "failed to update run status", "event_projection", err)
		}
	}
	h.projectSessionPhase(c.Request().Context(), event.RunID, event.EventType, event.Payload)
	h.eventHub.Publish(event)
	return writeJSON(c, http.StatusAccepted, event)
}

func (h *Handler) projectSessionPhase(ctx context.Context, runID, eventType, payload string) {
	if h.phase == nil {
		return
	}
	phase := ""
	if eventType == "approval/requested" {
		phase = v1alpha1.PhaseWaitingForApproval
	} else {
		status, ok := runs.StatusForEvent(eventType, payload)
		if !ok {
			return
		}
		phase = v1alpha1.PhaseReady
		switch status {
		case runs.StatusFailed, runs.StatusInterrupted:
			phase = v1alpha1.PhaseFailed
		}
	}
	runStore, ok := h.repository.(interface {
		GetRun(context.Context, string) (runs.Run, bool, error)
	})
	if !ok {
		return
	}
	run, found, err := runStore.GetRun(ctx, runID)
	if err != nil || !found {
		return
	}
	_ = h.phase.SetPhase(ctx, run.ConversationID, phase)
}

func (h *Handler) checkHealth(c echo.Context) error {
	return writeJSON(c, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) receiveHeartbeat(c echo.Context) error {
	at := time.Now().UTC()
	if err := h.activity.Touch(c.Request().Context(), c.Param("conversationID"), at); err != nil {
		return writeOperationalError(c, http.StatusBadGateway, "failed to record heartbeat", "session_heartbeat", err)
	}
	if recorder, ok := h.repository.(conversation.ActivityRecorder); ok {
		if err := recorder.TouchConversation(c.Request().Context(), c.Param("conversationID"), at); err != nil {
			return writeOperationalError(c, http.StatusBadGateway, "failed to record conversation activity", "conversation_heartbeat", err)
		}
	}
	return writeJSON(c, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (h *Handler) respondApproval(c echo.Context) error {
	var request struct {
		Response json.RawMessage `json:"response"`
	}
	if err := decodeJSON(c, &request); err != nil {
		return err
	}
	if len(request.Response) == 0 || !json.Valid(request.Response) {
		return writeError(c, http.StatusBadRequest, "response must be valid JSON")
	}
	var response any
	if err := json.Unmarshal(request.Response, &response); err != nil {
		return writeError(c, http.StatusBadRequest, "response must be valid JSON")
	}
	if err := h.approvalResponder.RespondApproval(c.Request().Context(), c.Param("runID"), c.Param("requestID"), response); err != nil {
		return writeOperationalError(c, http.StatusBadGateway, "failed to respond to approval", "approval_response", err)
	}
	return writeJSON(c, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (h *Handler) listPendingApprovals(c echo.Context) error {
	lister, ok := h.repository.(ApprovalLister)
	if !ok {
		return writeError(c, http.StatusNotImplemented, "approval listing is not supported")
	}
	conversationID := c.Param("id")
	if _, err := h.repository.GetConversation(c.Request().Context(), conversationID); err != nil {
		return writeRepositoryError(c, err)
	}
	approvals, err := lister.ListPendingApprovals(c.Request().Context(), conversationID)
	if err != nil {
		return writeRepositoryError(c, err)
	}
	return writeJSON(c, http.StatusOK, map[string]any{"approvals": approvals})
}

func (h *Handler) createConversation(c echo.Context) error {
	var request struct {
		Repository *conversation.RepositoryConfig `json:"repository,omitempty"`
	}
	if c.Request().ContentLength != 0 {
		if err := decodeJSON(c, &request); err != nil {
			return err
		}
	}
	if request.Repository != nil {
		request.Repository.URL = strings.TrimSpace(request.Repository.URL)
		request.Repository.Revision = strings.TrimSpace(request.Repository.Revision)
		if request.Repository.URL == "" || request.Repository.Revision == "" {
			return writeError(c, http.StatusBadRequest, "repository url and revision are required together")
		}
	}
	var value conversation.Conversation
	var err error
	if configured, ok := h.repository.(conversation.RepositoryConfigurer); ok {
		value, err = configured.CreateConversationWithRepository(c.Request().Context(), request.Repository)
	} else if request.Repository != nil {
		return writeError(c, http.StatusInternalServerError, "repository configuration storage is not supported")
	} else {
		value, err = h.repository.CreateConversation(c.Request().Context())
	}
	if err != nil {
		return writeRepositoryError(c, err)
	}
	return writeJSON(c, http.StatusCreated, conversationResponseFrom(value))
}

func (h *Handler) listConversations(c echo.Context) error {
	lister, ok := h.repository.(conversation.Lister)
	if !ok {
		return writeError(c, http.StatusNotImplemented, "conversation listing is not supported")
	}
	values, err := lister.ListConversations(c.Request().Context())
	if err != nil {
		return writeRepositoryError(c, err)
	}
	response := make([]conversationListResponse, 0, len(values))
	for _, value := range values {
		response = append(response, conversationListResponse{
			ID:        value.ID,
			Title:     value.Title,
			Status:    value.Status,
			CreatedAt: value.CreatedAt,
			UpdatedAt: value.UpdatedAt,
		})
	}
	return writeJSON(c, http.StatusOK, map[string]any{"conversations": response})
}

// archiveConversation removes the Kubernetes session and archives its history.
func (h *Handler) archiveConversation(c echo.Context) error {
	archiver, ok := h.repository.(conversation.Archiver)
	if !ok {
		return writeError(c, http.StatusNotImplemented, "conversation archiving is not supported")
	}
	id := c.Param("id")
	if _, err := h.repository.GetConversation(c.Request().Context(), id); err != nil {
		return writeRepositoryError(c, err)
	}
	if h.activator != nil {
		cleaner, ok := h.activator.(interface {
			DeleteSession(context.Context, string) error
		})
		if !ok {
			return writeError(c, http.StatusServiceUnavailable, "session deletion is unavailable")
		}
		if err := cleaner.DeleteSession(c.Request().Context(), id); err != nil {
			return writeError(c, http.StatusServiceUnavailable, "failed to delete session")
		}
	}
	if err := archiver.ArchiveConversation(c.Request().Context(), id); err != nil {
		return writeRepositoryError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
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

func (h *Handler) listActiveRuns(c echo.Context) error {
	lister, ok := h.repository.(ActiveRunLister)
	if !ok {
		return writeError(c, http.StatusNotImplemented, "active run listing is not supported")
	}
	conversationID := c.Param("id")
	conversationValue, err := h.repository.GetConversation(c.Request().Context(), conversationID)
	if err != nil {
		return writeRepositoryError(c, err)
	}
	if conversationValue.Status == "archived" {
		return writeError(c, http.StatusNotFound, "conversation not found")
	}
	active, err := lister.ListActiveRuns(c.Request().Context(), conversationID)
	if err != nil {
		return writeRepositoryError(c, err)
	}
	return writeJSON(c, http.StatusOK, map[string]any{"runs": active})
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
	message, created, err := h.repository.CreateMessage(c.Request().Context(), conversationID, conversation.NewMessage{
		Content:         request.Content,
		ClientMessageID: request.ClientMessageID,
	})
	if err != nil {
		return writeRepositoryError(c, err)
	}
	dispatchable := message.Status == "queued"
	if dispatchable && h.dispatcher != nil {
		if h.statusStore == nil {
			return writeError(c, http.StatusInternalServerError, "message status store is not configured")
		}
		claimed, err := h.statusStore.ClaimQueuedMessage(c.Request().Context(), message.ID)
		if err != nil {
			return writeOperationalError(c, http.StatusInternalServerError, "failed to claim message for dispatch", "message_claim", err)
		}
		if !claimed {
			dispatchable = false
			messages, err := h.repository.ListMessages(c.Request().Context(), conversationID)
			if err != nil {
				return writeRepositoryError(c, err)
			}
			for _, current := range messages {
				if current.ID == message.ID {
					message = current
					break
				}
			}
			if message.Status == "queued" {
				return writeJSON(c, http.StatusAccepted, map[string]any{"message": messageResponseFrom(message), "queued": true})
			}
		} else {
			message.Status = "running"
		}
	}
	if dispatchable && h.activator != nil {
		conversationValue, err := h.repository.GetConversation(c.Request().Context(), conversationID)
		if err != nil {
			h.resetQueuedMessage(c, message.ID)
			return writeRepositoryError(c, err)
		}
		var repository *session.Repository
		if conversationValue.Repository != nil {
			repository = &session.Repository{URL: conversationValue.Repository.URL, Revision: conversationValue.Repository.Revision}
		}
		if err := h.activator.Activate(c.Request().Context(), session.Activation{ConversationID: conversationID, RequestedAt: time.Now().UTC(), Repository: repository}); err != nil {
			if errors.Is(err, session.ErrSessionHasActiveRun) && h.dispatcher != nil {
				h.resetQueuedMessage(c, message.ID)
				message.Status = "queued"
				return writeJSON(c, http.StatusAccepted, map[string]any{"message": messageResponseFrom(message), "queued": true})
			}
			if h.statusStore != nil && h.dispatcher != nil {
				_ = h.statusStore.SetMessageStatus(c.Request().Context(), message.ID, "failed")
			}
			return writeOperationalError(c, http.StatusServiceUnavailable, "failed to activate session", "session_activate", err)
		}
	}
	if dispatchable && h.dispatcher != nil {
		run, err := h.dispatchClaimedMessage(c.Request().Context(), message, request.ThreadID)
		if err != nil {
			_ = h.statusStore.SetMessageStatus(c.Request().Context(), message.ID, "failed")
			if h.phase != nil {
				_ = h.phase.SetPhase(c.Request().Context(), message.ConversationID, v1alpha1.PhaseFailed)
			}
			return writeOperationalErrorWithAttributes(c, http.StatusBadGateway, "failed to start run", "run_start", err,
				"conversation_id", message.ConversationID,
				"message_id", message.ID,
			)
		}
		message.Status = "running"
		return writeJSON(c, http.StatusAccepted, map[string]any{
			"message": messageResponseFrom(message),
			"run":     run,
		})
	}
	status := http.StatusOK
	if created && message.Status == "queued" {
		status = http.StatusAccepted
	}
	return writeJSON(c, status, messageResponseFrom(message))
}

func (h *Handler) resetQueuedMessage(c echo.Context, messageID string) {
	if h.statusStore != nil {
		_ = h.statusStore.SetMessageStatus(c.Request().Context(), messageID, "queued")
	}
}

type createMessageRequest struct {
	Content         string `json:"content"`
	ClientMessageID string `json:"clientMessageId"`
	ThreadID        string `json:"threadId,omitempty"`
}

type conversationResponse struct {
	ID         string                         `json:"id"`
	Title      string                         `json:"title"`
	Status     string                         `json:"status"`
	Repository *conversation.RepositoryConfig `json:"repository,omitempty"`
	CreatedAt  time.Time                      `json:"createdAt"`
	UpdatedAt  time.Time                      `json:"updatedAt"`
	Messages   []messageResponse              `json:"messages,omitempty"`
}

type conversationListResponse struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
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
	title := value.Title
	if title == "" {
		for _, message := range value.Messages {
			if message.Role == "user" {
				title = conversationTitle(message.Content)
				break
			}
		}
	}
	response := conversationResponse{
		ID:         value.ID,
		Title:      title,
		Status:     value.Status,
		Repository: value.Repository,
		CreatedAt:  value.CreatedAt,
		UpdatedAt:  value.UpdatedAt,
	}
	if len(value.Messages) > 0 {
		response.Messages = make([]messageResponse, 0, len(value.Messages))
		for _, message := range value.Messages {
			response.Messages = append(response.Messages, messageResponseFrom(message))
		}
	}
	return response
}

func conversationTitle(content string) string {
	content = strings.Join(strings.Fields(content), " ")
	runes := []rune(content)
	if len(runes) > 48 {
		return string(runes[:48]) + "…"
	}
	return content
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

func writeOperationalError(c echo.Context, status int, message, operation string, cause error) error {
	return writeOperationalErrorWithAttributes(c, status, message, operation, cause)
}

func writeOperationalErrorWithAttributes(c echo.Context, status int, message, operation string, cause error, attributes ...any) error {
	telemetry.RecordError(c.Request().Context(), cause)
	attributes = append(attributes,
		"operation", operation,
		"http.response.status_code", status,
		"error.type", logging.ErrorType(cause),
	)
	slog.ErrorContext(c.Request().Context(), message, attributes...)
	return writeError(c, status, message)
}

func writeRepositoryError(c echo.Context, err error) error {
	if errors.Is(err, conversation.ErrArchived) {
		return writeError(c, http.StatusGone, "conversation has been archived")
	}
	if errors.Is(err, conversation.ErrNotFound) {
		return writeError(c, http.StatusNotFound, "conversation not found")
	}
	return writeOperationalError(c, http.StatusInternalServerError, "internal server error", "repository", err)
}

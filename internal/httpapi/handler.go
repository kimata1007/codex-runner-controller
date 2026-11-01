package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/labstack/echo/v4"
)

const maxRequestBodyBytes = 1 << 20

type Server struct {
	store *Store
}

type Store struct {
	mu            sync.RWMutex
	conversations map[string]*Conversation
	sequence      uint64
}

type Conversation struct {
	ID        string    `json:"id"`
	Mode      string    `json:"mode"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Messages  []Message `json:"messages,omitempty"`
}

type Message struct {
	ID              string    `json:"id"`
	ConversationID  string    `json:"conversationId"`
	Role            string    `json:"role"`
	Content         string    `json:"content"`
	ClientMessageID string    `json:"clientMessageId,omitempty"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"createdAt"`
}

type createConversationRequest struct {
	Mode string `json:"mode"`
}

type createMessageRequest struct {
	Content         string `json:"content"`
	ClientMessageID string `json:"clientMessageId"`
}

func NewServer() *Server {
	return &Server{store: &Store{conversations: make(map[string]*Conversation)}}
}

func (s *Server) Routes() http.Handler {
	e := echo.New()
	e.HideBanner = true
	e.Any("/healthz", s.handleHealth)
	e.Any("/readyz", s.handleHealth)
	e.Any("/v1/conversations", s.handleConversations)
	e.Any("/v1/conversations/:id", s.handleConversation)
	e.Any("/v1/conversations/:id/messages", s.handleMessages)
	return e
}

func (s *Server) handleHealth(c echo.Context) error {
	return writeJSON(c, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleConversations(c echo.Context) error {
	if c.Request().Method != http.MethodPost {
		return methodNotAllowed(c, http.MethodPost)
	}

	var request createConversationRequest
	if err := decodeJSON(c, &request); err != nil {
		return err
	}
	if request.Mode == "" {
		request.Mode = "chat"
	}
	if request.Mode != "chat" && request.Mode != "implement" {
		return writeError(c, http.StatusBadRequest, "mode must be chat or implement")
	}

	now := time.Now().UTC()
	id := s.store.newID("conversation")
	conversation := &Conversation{ID: id, Mode: request.Mode, Status: "active", CreatedAt: now, UpdatedAt: now}

	s.store.mu.Lock()
	s.store.conversations[id] = conversation
	s.store.mu.Unlock()

	return writeJSON(c, http.StatusCreated, conversation)
}

func (s *Server) handleConversation(c echo.Context) error {
	if c.Request().Method != http.MethodGet {
		return methodNotAllowed(c, http.MethodGet)
	}
	return s.getConversation(c, c.Param("id"))
}

func (s *Server) getConversation(c echo.Context, id string) error {
	s.store.mu.RLock()
	conversation, ok := s.store.conversations[id]
	if ok {
		conversation = cloneConversation(conversation)
	}
	s.store.mu.RUnlock()
	if !ok {
		return writeError(c, http.StatusNotFound, "conversation not found")
	}
	return writeJSON(c, http.StatusOK, conversation)
}

func (s *Server) handleMessages(c echo.Context) error {
	conversationID := c.Param("id")
	s.store.mu.RLock()
	conversation, ok := s.store.conversations[conversationID]
	s.store.mu.RUnlock()
	if !ok {
		return writeError(c, http.StatusNotFound, "conversation not found")
	}

	switch c.Request().Method {
	case http.MethodGet:
		s.store.mu.RLock()
		messages := append([]Message(nil), conversation.Messages...)
		s.store.mu.RUnlock()
		return writeJSON(c, http.StatusOK, map[string]any{"messages": messages})
	case http.MethodPost:
		return s.createMessage(c, conversationID)
	default:
		return methodNotAllowed(c, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) createMessage(c echo.Context, conversationID string) error {
	var request createMessageRequest
	if err := decodeJSON(c, &request); err != nil {
		return err
	}
	request.Content = strings.TrimSpace(request.Content)
	if request.Content == "" {
		return writeError(c, http.StatusBadRequest, "content is required")
	}

	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	conversation, ok := s.store.conversations[conversationID]
	if !ok {
		return writeError(c, http.StatusNotFound, "conversation not found")
	}
	if request.ClientMessageID != "" {
		for _, message := range conversation.Messages {
			if message.ClientMessageID == request.ClientMessageID {
				return writeJSON(c, http.StatusOK, message)
			}
		}
	}

	now := time.Now().UTC()
	message := Message{
		ID:              s.store.newID("message"),
		ConversationID:  conversationID,
		Role:            "user",
		Content:         request.Content,
		ClientMessageID: request.ClientMessageID,
		Status:          "queued",
		CreatedAt:       now,
	}
	conversation.Messages = append(conversation.Messages, message)
	conversation.UpdatedAt = now
	return writeJSON(c, http.StatusAccepted, message)
}

func (s *Store) newID(prefix string) string {
	sequence := atomic.AddUint64(&s.sequence, 1)
	return fmt.Sprintf("%s-%d", prefix, sequence)
}

func cloneConversation(conversation *Conversation) *Conversation {
	clone := *conversation
	clone.Messages = append([]Message(nil), conversation.Messages...)
	return &clone
}

func decodeJSON(c echo.Context, destination any) error {
	r := c.Request()
	r.Body = http.MaxBytesReader(c.Response().Writer, r.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		if err == io.EOF {
			return writeError(c, http.StatusBadRequest, "request body is required")
		} else {
			return writeError(c, http.StatusBadRequest, "invalid JSON request")
		}
	}
	return nil
}

func methodNotAllowed(c echo.Context, methods ...string) error {
	c.Response().Header().Set("Allow", strings.Join(methods, ", "))
	return writeError(c, http.StatusMethodNotAllowed, "method not allowed")
}

func writeJSON(c echo.Context, status int, value any) error {
	return c.JSON(status, value)
}

func writeError(c echo.Context, status int, message string) error {
	return writeJSON(c, status, map[string]string{"error": message})
}

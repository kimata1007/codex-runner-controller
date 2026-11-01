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
	e.GET("/healthz", s.checkHealth)
	e.GET("/readyz", s.checkHealth)
	e.POST("/v1/conversations", s.createConversation)
	e.GET("/v1/conversations/:id", s.getConversation)
	e.GET("/v1/conversations/:id/messages", s.getMessages)
	e.POST("/v1/conversations/:id/messages", s.createMessage)
	return e
}

func (s *Server) checkHealth(c echo.Context) error {
	return writeJSON(c, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) createConversation(c echo.Context) error {
	now := time.Now().UTC()
	id := s.store.newID("conversation")
	conversation := &Conversation{ID: id, Status: "active", CreatedAt: now, UpdatedAt: now}

	s.store.mu.Lock()
	s.store.conversations[id] = conversation
	s.store.mu.Unlock()

	return writeJSON(c, http.StatusCreated, conversation)
}

func (s *Server) getConversation(c echo.Context) error {
	id := c.Param("id")
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

func (s *Server) getMessages(c echo.Context) error {
	conversationID := c.Param("id")
	s.store.mu.RLock()
	conversation, ok := s.store.conversations[conversationID]
	s.store.mu.RUnlock()
	if !ok {
		return writeError(c, http.StatusNotFound, "conversation not found")
	}

	s.store.mu.RLock()
	messages := append([]Message(nil), conversation.Messages...)
	s.store.mu.RUnlock()
	return writeJSON(c, http.StatusOK, map[string]any{"messages": messages})
}

func (s *Server) createMessage(c echo.Context) error {
	conversationID := c.Param("id")
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

func writeJSON(c echo.Context, status int, value any) error {
	return c.JSON(status, value)
}

func writeError(c echo.Context, status int, message string) error {
	return writeJSON(c, status, map[string]string{"error": message})
}

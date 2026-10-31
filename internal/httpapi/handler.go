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
	store ConversationRepository
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

type MessageInput struct {
	Content         string `json:"content"`
	ClientMessageID string `json:"clientMessageId"`
}

type ConversationRepository interface {
	CreateConversation() *Conversation
	GetConversation(id string) (*Conversation, bool)
	ListMessages(id string) ([]Message, bool)
	CreateMessage(id string, input MessageInput) (Message, bool, error)
}

func NewServer() *Server {
	return NewServerWithRepository(&Store{conversations: make(map[string]*Conversation)})
}

func NewServerWithRepository(repository ConversationRepository) *Server {
	return &Server{store: repository}
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
	conversation := s.store.CreateConversation()
	return writeJSON(c, http.StatusCreated, conversation)
}

func (s *Server) getConversation(c echo.Context) error {
	conversation, ok := s.store.GetConversation(c.Param("id"))
	if !ok {
		return writeError(c, http.StatusNotFound, "conversation not found")
	}
	return writeJSON(c, http.StatusOK, conversation)
}

func (s *Server) getMessages(c echo.Context) error {
	conversationID := c.Param("id")
	_, ok := s.store.GetConversation(conversationID)
	if !ok {
		return writeError(c, http.StatusNotFound, "conversation not found")
	}

	messages, _ := s.store.ListMessages(conversationID)
	return writeJSON(c, http.StatusOK, map[string]any{"messages": messages})
}

func (s *Server) createMessage(c echo.Context) error {
	var request MessageInput
	if err := decodeJSON(c, &request); err != nil {
		return err
	}
	request.Content = strings.TrimSpace(request.Content)
	if request.Content == "" {
		return writeError(c, http.StatusBadRequest, "content is required")
	}

	message, created, err := s.store.CreateMessage(c.Param("id"), request)
	if err != nil {
		return writeError(c, http.StatusNotFound, "conversation not found")
	}
	if created {
		return writeJSON(c, http.StatusAccepted, message)
	}
	return writeJSON(c, http.StatusOK, message)
}

func (s *Store) CreateConversation() *Conversation {
	now := time.Now().UTC()
	id := s.newID("conversation")
	conversation := &Conversation{ID: id, Status: "active", CreatedAt: now, UpdatedAt: now}

	s.mu.Lock()
	s.conversations[id] = conversation
	s.mu.Unlock()
	return conversation
}

func (s *Store) GetConversation(id string) (*Conversation, bool) {
	s.mu.RLock()
	conversation, ok := s.conversations[id]
	if ok {
		conversation = cloneConversation(conversation)
	}
	s.mu.RUnlock()
	return conversation, ok
}

func (s *Store) ListMessages(id string) ([]Message, bool) {
	s.mu.RLock()
	conversation, ok := s.conversations[id]
	if ok {
		messages := append([]Message(nil), conversation.Messages...)
		s.mu.RUnlock()
		return messages, true
	}
	s.mu.RUnlock()
	return nil, false
}

func (s *Store) CreateMessage(id string, input MessageInput) (Message, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	conversation, ok := s.conversations[id]
	if !ok {
		return Message{}, false, fmt.Errorf("conversation %q not found", id)
	}
	if input.ClientMessageID != "" {
		for _, message := range conversation.Messages {
			if message.ClientMessageID == input.ClientMessageID {
				return message, false, nil
			}
		}
	}

	now := time.Now().UTC()
	message := Message{
		ID:              s.newID("message"),
		ConversationID:  id,
		Role:            "user",
		Content:         input.Content,
		ClientMessageID: input.ClientMessageID,
		Status:          "queued",
		CreatedAt:       now,
	}
	conversation.Messages = append(conversation.Messages, message)
	conversation.UpdatedAt = now
	return message, true, nil
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

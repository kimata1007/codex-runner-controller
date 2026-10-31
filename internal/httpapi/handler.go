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
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/readyz", s.handleHealth)
	mux.HandleFunc("/v1/conversations", s.handleConversations)
	mux.HandleFunc("/v1/conversations/", s.handleConversation)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleConversations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	var request createConversationRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	if request.Mode == "" {
		request.Mode = "chat"
	}
	if request.Mode != "chat" && request.Mode != "implement" {
		writeError(w, http.StatusBadRequest, "mode must be chat or implement")
		return
	}

	now := time.Now().UTC()
	id := s.store.newID("conversation")
	conversation := &Conversation{ID: id, Mode: request.Mode, Status: "active", CreatedAt: now, UpdatedAt: now}

	s.store.mu.Lock()
	s.store.conversations[id] = conversation
	s.store.mu.Unlock()

	writeJSON(w, http.StatusCreated, conversation)
}

func (s *Server) handleConversation(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/conversations/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}

	id := parts[0]
	if len(parts) == 1 && r.Method == http.MethodGet {
		s.getConversation(w, id)
		return
	}
	if len(parts) == 2 && parts[1] == "messages" {
		s.handleMessages(w, r, id)
		return
	}

	writeError(w, http.StatusNotFound, "route not found")
}

func (s *Server) getConversation(w http.ResponseWriter, id string) {
	s.store.mu.RLock()
	conversation, ok := s.store.conversations[id]
	if ok {
		conversation = cloneConversation(conversation)
	}
	s.store.mu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	writeJSON(w, http.StatusOK, conversation)
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request, conversationID string) {
	s.store.mu.RLock()
	conversation, ok := s.store.conversations[conversationID]
	s.store.mu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.store.mu.RLock()
		messages := append([]Message(nil), conversation.Messages...)
		s.store.mu.RUnlock()
		writeJSON(w, http.StatusOK, map[string]any{"messages": messages})
	case http.MethodPost:
		s.createMessage(w, r, conversationID)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) createMessage(w http.ResponseWriter, r *http.Request, conversationID string) {
	var request createMessageRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	request.Content = strings.TrimSpace(request.Content)
	if request.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	conversation, ok := s.store.conversations[conversationID]
	if !ok {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	if request.ClientMessageID != "" {
		for _, message := range conversation.Messages {
			if message.ClientMessageID == request.ClientMessageID {
				writeJSON(w, http.StatusOK, message)
				return
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
	writeJSON(w, http.StatusAccepted, message)
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

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		if err == io.EOF {
			writeError(w, http.StatusBadRequest, "request body is required")
		} else {
			writeError(w, http.StatusBadRequest, "invalid JSON request")
		}
		return err
	}
	return nil
}

func methodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

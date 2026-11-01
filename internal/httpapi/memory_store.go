package httpapi

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type Store struct {
	mu            sync.RWMutex
	conversations map[string]*Conversation
	sequence      uint64
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

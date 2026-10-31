package memory

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/conversation"
)

var _ conversation.Repository = (*Repository)(nil)

type Repository struct {
	mu            sync.RWMutex
	conversations map[string]*conversation.Conversation
	sequence      uint64
}

func NewRepository() *Repository {
	return &Repository{conversations: make(map[string]*conversation.Conversation)}
}

func (r *Repository) CreateConversation(ctx context.Context) (conversation.Conversation, error) {
	return r.CreateConversationWithRepository(ctx, nil)
}

func (r *Repository) CreateConversationWithRepository(ctx context.Context, repository *conversation.RepositoryConfig) (conversation.Conversation, error) {
	if err := ctx.Err(); err != nil {
		return conversation.Conversation{}, err
	}

	now := time.Now().UTC()
	value := conversation.Conversation{
		ID:         r.newID("conversation"),
		Status:     "active",
		Repository: repository,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	r.mu.Lock()
	r.conversations[value.ID] = &value
	r.mu.Unlock()
	return value, nil
}

func (r *Repository) GetConversation(ctx context.Context, id string) (conversation.Conversation, error) {
	if err := ctx.Err(); err != nil {
		return conversation.Conversation{}, err
	}

	r.mu.RLock()
	value, ok := r.conversations[id]
	if ok {
		value = cloneConversation(value)
	}
	r.mu.RUnlock()
	if !ok {
		return conversation.Conversation{}, fmt.Errorf("%w: %s", conversation.ErrNotFound, id)
	}
	return *value, nil
}

func (r *Repository) ListMessages(ctx context.Context, id string) ([]conversation.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mu.RLock()
	value, ok := r.conversations[id]
	if ok {
		messages := append([]conversation.Message(nil), value.Messages...)
		r.mu.RUnlock()
		return messages, nil
	}
	r.mu.RUnlock()
	return nil, fmt.Errorf("%w: %s", conversation.ErrNotFound, id)
}

func (r *Repository) CreateMessage(ctx context.Context, id string, input conversation.NewMessage) (conversation.Message, bool, error) {
	if err := ctx.Err(); err != nil {
		return conversation.Message{}, false, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.conversations[id]
	if !ok {
		return conversation.Message{}, false, fmt.Errorf("%w: %s", conversation.ErrNotFound, id)
	}
	if input.ClientMessageID != "" {
		for _, message := range value.Messages {
			if message.ClientMessageID == input.ClientMessageID {
				return message, false, nil
			}
		}
	}

	now := time.Now().UTC()
	message := conversation.Message{
		ID:              r.newID("message"),
		ConversationID:  id,
		Role:            "user",
		Content:         input.Content,
		ClientMessageID: input.ClientMessageID,
		Status:          "queued",
		CreatedAt:       now,
	}
	value.Messages = append(value.Messages, message)
	value.UpdatedAt = now
	return message, true, nil
}

func (r *Repository) SetMessageStatus(ctx context.Context, id, status string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, value := range r.conversations {
		for index := range value.Messages {
			if value.Messages[index].ID == id {
				value.Messages[index].Status = status
				value.UpdatedAt = time.Now().UTC()
				return nil
			}
		}
	}
	return fmt.Errorf("message %q not found", id)
}

func (r *Repository) newID(prefix string) string {
	sequence := atomic.AddUint64(&r.sequence, 1)
	return fmt.Sprintf("%s-%d", prefix, sequence)
}

func cloneConversation(value *conversation.Conversation) *conversation.Conversation {
	clone := *value
	clone.Messages = append([]conversation.Message(nil), value.Messages...)
	return &clone
}

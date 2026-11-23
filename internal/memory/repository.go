package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/conversation"
	"github.com/kimata1007/codex-runner-controller/internal/session"
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
	if value.Status == "archived" || !value.UpdatedAt.Add(session.DefaultRetentionPeriod).After(time.Now().UTC()) {
		return conversation.Conversation{}, fmt.Errorf("%w: %s", conversation.ErrArchived, id)
	}
	return *value, nil
}

func (r *Repository) ListConversations(ctx context.Context) ([]conversation.Conversation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	values := make([]conversation.Conversation, 0, len(r.conversations))
	for _, value := range r.conversations {
		if value.Status == "archived" || !value.UpdatedAt.Add(session.DefaultRetentionPeriod).After(time.Now().UTC()) {
			continue
		}
		clone := cloneConversation(value)
		for _, message := range clone.Messages {
			if message.Role == "user" {
				clone.Title = conversationTitle(message.Content)
				break
			}
		}
		values = append(values, *clone)
	}
	r.mu.RUnlock()
	sort.Slice(values, func(i, j int) bool { return values[i].UpdatedAt.After(values[j].UpdatedAt) })
	if len(values) > 100 {
		values = values[:100]
	}
	return values, nil
}

func (r *Repository) ListMessages(ctx context.Context, id string) ([]conversation.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mu.RLock()
	value, ok := r.conversations[id]
	if ok {
		if value.Status == "archived" || !value.UpdatedAt.Add(session.DefaultRetentionPeriod).After(time.Now().UTC()) {
			r.mu.RUnlock()
			return nil, fmt.Errorf("%w: %s", conversation.ErrArchived, id)
		}
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
	if value.Status == "archived" || !value.UpdatedAt.Add(session.DefaultRetentionPeriod).After(time.Now().UTC()) {
		return conversation.Message{}, false, fmt.Errorf("%w: %s", conversation.ErrArchived, id)
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
	value.Status = "active"
	value.UpdatedAt = now
	return message, true, nil
}

func (r *Repository) ClaimQueuedMessage(ctx context.Context, id string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, value := range r.conversations {
		for index := range value.Messages {
			if value.Messages[index].ID != id {
				continue
			}
			if value.Messages[index].Status != "queued" {
				return false, nil
			}
			for activeIndex := range value.Messages {
				if activeIndex != index && value.Messages[activeIndex].Role == "user" && value.Messages[activeIndex].Status == "running" {
					return false, nil
				}
			}
			value.Messages[index].Status = "running"
			value.UpdatedAt = time.Now().UTC()
			return true, nil
		}
	}
	return false, fmt.Errorf("message %q not found", id)
}

func (r *Repository) ListQueuedConversationIDs(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0)
	for id, value := range r.conversations {
		for _, message := range value.Messages {
			if message.Role == "user" && message.Status == "queued" {
				ids = append(ids, id)
				break
			}
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func (r *Repository) ClaimNextQueuedMessage(ctx context.Context, conversationID string) (conversation.Message, bool, error) {
	if err := ctx.Err(); err != nil {
		return conversation.Message{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.conversations[conversationID]
	if !ok {
		return conversation.Message{}, false, fmt.Errorf("%w: %s", conversation.ErrNotFound, conversationID)
	}
	for _, message := range value.Messages {
		if message.Role == "user" && message.Status == "running" {
			return conversation.Message{}, false, nil
		}
	}
	for index := range value.Messages {
		message := &value.Messages[index]
		if message.Role == "user" && message.Status == "queued" {
			message.Status = "running"
			value.UpdatedAt = time.Now().UTC()
			return *message, true, nil
		}
	}
	return conversation.Message{}, false, nil
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
	if value.Repository != nil {
		repository := *value.Repository
		clone.Repository = &repository
	}
	clone.Messages = append([]conversation.Message(nil), value.Messages...)
	return &clone
}

func conversationTitle(content string) string {
	content = strings.Join(strings.Fields(content), " ")
	runes := []rune(content)
	if len(runes) > 48 {
		return string(runes[:48]) + "…"
	}
	return content
}

func (r *Repository) ArchiveConversation(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.conversations[id]
	if !ok {
		return fmt.Errorf("%w: %s", conversation.ErrNotFound, id)
	}
	value.Status = "archived"
	value.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *Repository) TouchConversation(ctx context.Context, id string, at time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.conversations[id]
	if !ok {
		return fmt.Errorf("%w: %s", conversation.ErrNotFound, id)
	}
	if value.Status == "archived" {
		return fmt.Errorf("%w: %s", conversation.ErrArchived, id)
	}
	if at.After(value.UpdatedAt) {
		value.UpdatedAt = at.UTC()
	}
	return nil
}

func (r *Repository) ArchiveInactiveConversations(ctx context.Context, cutoff time.Time) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var cleanup []string
	for id, value := range r.conversations {
		if value.UpdatedAt.After(cutoff) {
			continue
		}
		value.Status = "archived"
		cleanup = append(cleanup, id)
	}
	return cleanup, nil
}

package libsql

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/conversation"
	"github.com/kimata1007/codex-runner-controller/internal/db"
)

var _ conversation.Repository = (*Repository)(nil)

type Repository struct {
	client *Client
	userID string
	seq    uint64
}

func NewRepository(client *Client, userID string) *Repository {
	if userID == "" {
		userID = "default"
	}
	return &Repository{client: client, userID: userID}
}

func (r *Repository) Migrate(ctx context.Context) error {
	return r.client.ApplySchema(ctx, db.InitialSchema)
}

func (r *Repository) CreateConversation(ctx context.Context) (conversation.Conversation, error) {
	return r.CreateConversationWithRepository(ctx, nil)
}

func (r *Repository) CreateConversationWithRepository(ctx context.Context, repository *conversation.RepositoryConfig) (conversation.Conversation, error) {
	if err := ctx.Err(); err != nil {
		return conversation.Conversation{}, err
	}

	now := time.Now().UTC()
	id := r.newID("conversation")
	_, err := r.client.Exec(ctx, `INSERT INTO conversations
		(id, user_id, title, status, codex_session_name, created_at, updated_at)
		VALUES (?, ?, '', 'active', ?, ?, ?)`, []Value{
		Text(id), Text(r.userID), Text(id), Text(now.Format(time.RFC3339Nano)), Text(now.Format(time.RFC3339Nano)),
	}, false)
	if err != nil {
		return conversation.Conversation{}, err
	}
	if repository != nil {
		if _, err := r.client.Exec(ctx, `INSERT INTO conversation_repositories (conversation_id, url, revision) VALUES (?, ?, ?)`, []Value{Text(id), Text(repository.URL), Text(repository.Revision)}, false); err != nil {
			return conversation.Conversation{}, err
		}
	}
	return conversation.Conversation{ID: id, Status: "active", CreatedAt: now, UpdatedAt: now, Repository: repository}, nil
}

func (r *Repository) GetConversation(ctx context.Context, id string) (conversation.Conversation, error) {
	result, err := r.client.Exec(ctx, `SELECT c.id, c.status, c.created_at, c.updated_at, r.url, r.revision
		FROM conversations c LEFT JOIN conversation_repositories r ON r.conversation_id = c.id
		WHERE c.id = ? AND c.user_id = ?`, []Value{Text(id), Text(r.userID)}, true)
	if err != nil {
		return conversation.Conversation{}, err
	}
	if len(result.Rows) == 0 {
		return conversation.Conversation{}, fmt.Errorf("%w: %s", conversation.ErrNotFound, id)
	}
	if len(result.Rows[0]) < 6 {
		return conversation.Conversation{}, fmt.Errorf("libsql: conversation row has %d columns", len(result.Rows[0]))
	}

	row := result.Rows[0]
	createdAt, err := parseTime(row[2])
	if err != nil {
		return conversation.Conversation{}, err
	}
	updatedAt, err := parseTime(row[3])
	if err != nil {
		return conversation.Conversation{}, err
	}
	value := conversation.Conversation{
		ID:        row[0].Value,
		Status:    row[1].Value,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
	if row[4].Type != "null" || row[5].Type != "null" {
		value.Repository = &conversation.RepositoryConfig{URL: row[4].Value, Revision: row[5].Value}
	}
	value.Messages, err = r.listMessages(ctx, id)
	if err != nil {
		return conversation.Conversation{}, err
	}
	return value, nil
}

func (r *Repository) ListConversations(ctx context.Context) ([]conversation.Conversation, error) {
	result, err := r.client.Exec(ctx, `SELECT c.id, c.status, c.created_at, c.updated_at, r.url, r.revision,
		COALESCE((SELECT m.content FROM messages m WHERE m.conversation_id = c.id AND m.role = 'user' ORDER BY m.sequence LIMIT 1), c.title, '')
		FROM conversations c
		LEFT JOIN conversation_repositories r ON r.conversation_id = c.id
		WHERE c.user_id = ?
		ORDER BY c.updated_at DESC
		LIMIT 100`, []Value{Text(r.userID)}, true)
	if err != nil {
		return nil, err
	}
	values := make([]conversation.Conversation, 0, len(result.Rows))
	for _, row := range result.Rows {
		if len(row) < 7 {
			return nil, fmt.Errorf("libsql: conversation list row has %d columns", len(row))
		}
		createdAt, err := parseTime(row[2])
		if err != nil {
			return nil, err
		}
		updatedAt, err := parseTime(row[3])
		if err != nil {
			return nil, err
		}
		value := conversation.Conversation{
			ID:        row[0].Value,
			Status:    row[1].Value,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
			Title:     conversationTitle(row[6].Value),
		}
		if row[4].Type != "null" || row[5].Type != "null" {
			value.Repository = &conversation.RepositoryConfig{URL: row[4].Value, Revision: row[5].Value}
		}
		values = append(values, value)
	}
	return values, nil
}

func (r *Repository) ListMessages(ctx context.Context, id string) ([]conversation.Message, error) {
	exists, err := r.conversationExists(ctx, id)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("%w: %s", conversation.ErrNotFound, id)
	}
	return r.listMessages(ctx, id)
}

func (r *Repository) listMessages(ctx context.Context, id string) ([]conversation.Message, error) {
	result, err := r.client.Exec(ctx, `SELECT messages.id, messages.conversation_id, messages.role, messages.content,
		messages.client_message_id, messages.status, messages.created_at
		FROM messages
		JOIN conversations ON conversations.id = messages.conversation_id
		WHERE messages.conversation_id = ? AND conversations.user_id = ?
		ORDER BY messages.sequence`, []Value{Text(id), Text(r.userID)}, true)
	if err != nil {
		return nil, err
	}

	messages := make([]conversation.Message, 0, len(result.Rows))
	for _, row := range result.Rows {
		message, err := messageFromRow(row)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}

func (r *Repository) CreateMessage(ctx context.Context, id string, input conversation.NewMessage) (conversation.Message, bool, error) {
	if err := ctx.Err(); err != nil {
		return conversation.Message{}, false, err
	}

	messageID := r.newID("message")
	now := time.Now().UTC()
	clientMessageID := Null()
	if input.ClientMessageID != "" {
		clientMessageID = Text(input.ClientMessageID)
	}
	result, err := r.client.Exec(ctx, `INSERT INTO messages
		(id, conversation_id, role, content, sequence, status, client_message_id, created_at)
		SELECT ?, ?, 'user', ?, COALESCE((SELECT MAX(sequence) FROM messages WHERE conversation_id = ?), 0) + 1,
		       'queued', ?, ?
		WHERE EXISTS (SELECT 1 FROM conversations WHERE id = ? AND user_id = ?)
		ON CONFLICT(conversation_id, client_message_id) DO NOTHING`, []Value{
		Text(messageID), Text(id), Text(input.Content), Text(id), clientMessageID, Text(now.Format(time.RFC3339Nano)), Text(id), Text(r.userID),
	}, false)
	if err != nil {
		return conversation.Message{}, false, err
	}
	if result.AffectedRowCount > 0 {
		if _, err := r.client.Exec(ctx, `UPDATE conversations SET updated_at = ? WHERE id = ? AND user_id = ?`, []Value{
			Text(now.Format(time.RFC3339Nano)), Text(id), Text(r.userID),
		}, false); err != nil {
			return conversation.Message{}, false, err
		}
		return conversation.Message{
			ID:              messageID,
			ConversationID:  id,
			Role:            "user",
			Content:         input.Content,
			ClientMessageID: input.ClientMessageID,
			Status:          "queued",
			CreatedAt:       now,
		}, true, nil
	}

	exists, err := r.conversationExists(ctx, id)
	if err != nil {
		return conversation.Message{}, false, err
	}
	if !exists {
		return conversation.Message{}, false, fmt.Errorf("%w: %s", conversation.ErrNotFound, id)
	}
	if input.ClientMessageID == "" {
		return conversation.Message{}, false, fmt.Errorf("libsql: message was not inserted")
	}

	duplicate, err := r.client.Exec(ctx, `SELECT messages.id, messages.conversation_id, messages.role, messages.content,
		messages.client_message_id, messages.status, messages.created_at
		FROM messages
		JOIN conversations ON conversations.id = messages.conversation_id
		WHERE messages.conversation_id = ? AND messages.client_message_id = ? AND conversations.user_id = ?`, []Value{Text(id), Text(input.ClientMessageID), Text(r.userID)}, true)
	if err != nil {
		return conversation.Message{}, false, err
	}
	if len(duplicate.Rows) != 1 {
		return conversation.Message{}, false, fmt.Errorf("libsql: duplicate message was not found")
	}
	message, err := messageFromRow(duplicate.Rows[0])
	if err != nil {
		return conversation.Message{}, false, err
	}
	return message, false, nil
}

func (r *Repository) ClaimQueuedMessage(ctx context.Context, id string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	result, err := r.client.Exec(ctx, `UPDATE messages SET status = 'running' WHERE id = ? AND status = 'queued'`, []Value{Text(id)}, false)
	if err != nil {
		return false, err
	}
	return result.AffectedRowCount > 0, nil
}

func (r *Repository) SetMessageStatus(ctx context.Context, id, status string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	result, err := r.client.Exec(ctx, `UPDATE messages SET status = ? WHERE id = ?`, []Value{Text(status), Text(id)}, false)
	if err != nil {
		return err
	}
	if result.AffectedRowCount == 0 {
		return fmt.Errorf("message %q not found", id)
	}
	return nil
}

func (r *Repository) conversationExists(ctx context.Context, id string) (bool, error) {
	result, err := r.client.Exec(ctx, `SELECT id FROM conversations WHERE id = ? AND user_id = ?`, []Value{Text(id), Text(r.userID)}, true)
	if err != nil {
		return false, err
	}
	return len(result.Rows) > 0, nil
}

func (r *Repository) newID(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), atomic.AddUint64(&r.seq, 1))
}

func messageFromRow(row []Value) (conversation.Message, error) {
	if len(row) < 7 {
		return conversation.Message{}, fmt.Errorf("libsql: message row has %d columns", len(row))
	}
	createdAt, err := parseTime(row[6])
	if err != nil {
		return conversation.Message{}, err
	}
	message := conversation.Message{
		ID:             row[0].Value,
		ConversationID: row[1].Value,
		Role:           row[2].Value,
		Content:        row[3].Value,
		Status:         row[5].Value,
		CreatedAt:      createdAt,
	}
	if row[4].Type != "null" {
		message.ClientMessageID = row[4].Value
	}
	return message, nil
}

func parseTime(value Value) (time.Time, error) {
	if value.Type != "text" {
		return time.Time{}, fmt.Errorf("libsql: expected timestamp text, got %q", value.Type)
	}
	return time.Parse(time.RFC3339Nano, value.Value)
}

func conversationTitle(content string) string {
	content = strings.Join(strings.Fields(content), " ")
	runes := []rune(content)
	if len(runes) > 48 {
		return string(runes[:48]) + "…"
	}
	return content
}

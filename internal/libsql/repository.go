package libsql

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/db"
	"github.com/kimata1007/codex-runner-controller/internal/httpapi"
)

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

func (r *Repository) CreateConversation(mode string) (*httpapi.Conversation, error) {
	now := time.Now().UTC()
	id := r.newID("conversation")
	_, err := r.client.Exec(context.Background(), `INSERT INTO conversations
		(id, user_id, title, mode, status, codex_session_name, created_at, updated_at)
		VALUES (?, ?, '', ?, 'active', ?, ?, ?)`, []Value{
		Text(id), Text(r.userID), Text(mode), Text(id), Text(now.Format(time.RFC3339Nano)), Text(now.Format(time.RFC3339Nano)),
	}, false)
	if err != nil {
		return nil, err
	}
	return &httpapi.Conversation{ID: id, Mode: mode, Status: "active", CreatedAt: now, UpdatedAt: now}, nil
}

func (r *Repository) GetConversation(id string) (*httpapi.Conversation, bool, error) {
	result, err := r.client.Exec(context.Background(), `SELECT id, mode, status, created_at, updated_at
		FROM conversations WHERE id = ?`, []Value{Text(id)}, true)
	if err != nil {
		return nil, false, err
	}
	if len(result.Rows) == 0 {
		return nil, false, nil
	}
	row := result.Rows[0]
	if len(row) < 5 {
		return nil, false, fmt.Errorf("libsql: conversation row has %d columns", len(row))
	}
	createdAt, err := parseTime(row[3])
	if err != nil {
		return nil, false, err
	}
	updatedAt, err := parseTime(row[4])
	if err != nil {
		return nil, false, err
	}
	return &httpapi.Conversation{ID: valueString(row[0]), Mode: valueString(row[1]), Status: valueString(row[2]), CreatedAt: createdAt, UpdatedAt: updatedAt}, true, nil
}

func (r *Repository) ListMessages(id string) ([]httpapi.Message, bool, error) {
	conversation, ok, err := r.GetConversation(id)
	if err != nil || !ok {
		return nil, ok, err
	}
	_ = conversation
	result, err := r.client.Exec(context.Background(), `SELECT id, conversation_id, role, content, client_message_id, status, created_at
		FROM messages WHERE conversation_id = ? ORDER BY sequence`, []Value{Text(id)}, true)
	if err != nil {
		return nil, false, err
	}
	messages := make([]httpapi.Message, 0, len(result.Rows))
	for _, row := range result.Rows {
		if len(row) < 7 {
			return nil, false, fmt.Errorf("libsql: message row has %d columns", len(row))
		}
		createdAt, err := parseTime(row[6])
		if err != nil {
			return nil, false, err
		}
		message := httpapi.Message{ID: valueString(row[0]), ConversationID: valueString(row[1]), Role: valueString(row[2]), Content: valueString(row[3]), Status: valueString(row[5]), CreatedAt: createdAt}
		if row[4].Type != "null" {
			message.ClientMessageID = valueString(row[4])
		}
		messages = append(messages, message)
	}
	return messages, true, nil
}

func (r *Repository) CreateMessage(id string, input httpapi.MessageInput) (httpapi.Message, bool, error) {
	messageID := r.newID("message")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	clientMessageID := Null()
	if input.ClientMessageID != "" {
		clientMessageID = Text(input.ClientMessageID)
	}
	result, err := r.client.Exec(context.Background(), `INSERT INTO messages
		(id, conversation_id, role, content, sequence, status, client_message_id, created_at)
		VALUES (?, ?, 'user', ?, COALESCE((SELECT MAX(sequence) FROM messages WHERE conversation_id = ?), 0) + 1, 'queued', ?, ?)
		ON CONFLICT(conversation_id, client_message_id) DO NOTHING`, []Value{
		Text(messageID), Text(id), Text(input.Content), Text(id), clientMessageID, Text(now),
	}, false)
	if err != nil {
		return httpapi.Message{}, false, err
	}
	if result.AffectedRowCount == 0 && input.ClientMessageID == "" {
		return httpapi.Message{}, false, fmt.Errorf("libsql: message was not inserted")
	}
	if result.AffectedRowCount == 0 {
		messages, _, err := r.ListMessages(id)
		if err != nil {
			return httpapi.Message{}, false, err
		}
		for _, message := range messages {
			if message.ClientMessageID == input.ClientMessageID {
				return message, false, nil
			}
		}
		return httpapi.Message{}, false, fmt.Errorf("libsql: duplicate message was not found")
	}
	createdAt, _ := time.Parse(time.RFC3339Nano, now)
	return httpapi.Message{ID: messageID, ConversationID: id, Role: "user", Content: input.Content, ClientMessageID: input.ClientMessageID, Status: "queued", CreatedAt: createdAt}, true, nil
}

func (r *Repository) newID(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), atomic.AddUint64(&r.seq, 1))
}

func valueString(value Value) string {
	return value.Value
}

func parseTime(value Value) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, valueString(value))
}

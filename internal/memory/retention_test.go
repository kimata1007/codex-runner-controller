package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/conversation"
)

func TestArchiveInactiveConversationsKeepsHistoryButHidesConversation(t *testing.T) {
	repository := NewRepository()
	value, err := repository.CreateConversation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.CreateMessage(context.Background(), value.ID, conversation.NewMessage{Content: "kept in storage"}); err != nil {
		t.Fatal(err)
	}
	if err := repository.ArchiveInactiveConversations(context.Background(), time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	values, err := repository.ListConversations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 {
		t.Fatalf("visible conversations = %d, want 0", len(values))
	}
	if _, err := repository.GetConversation(context.Background(), value.ID); err == nil {
		t.Fatal("archived conversation remained readable through the active API")
	}
	if _, _, err := repository.CreateMessage(context.Background(), value.ID, conversation.NewMessage{Content: "new input"}); err == nil {
		t.Fatal("archived conversation accepted a new message")
	}

	repository.mu.RLock()
	defer repository.mu.RUnlock()
	stored := repository.conversations[value.ID]
	if stored.Status != "archived" || len(stored.Messages) != 1 || stored.Messages[0].Content != "kept in storage" {
		t.Fatalf("archived record = %+v", stored)
	}
}

func TestExpiredConversationIsHiddenBeforeArchiveSweep(t *testing.T) {
	repository := NewRepository()
	value, err := repository.CreateConversation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	repository.mu.Lock()
	repository.conversations[value.ID].UpdatedAt = time.Now().Add(-8 * 24 * time.Hour)
	repository.mu.Unlock()
	if _, err := repository.GetConversation(context.Background(), value.ID); err == nil {
		t.Fatal("expired conversation remained readable before the archive sweep")
	} else if !errors.Is(err, conversation.ErrArchived) {
		t.Fatalf("get error = %v, want archived", err)
	}
	values, err := repository.ListConversations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 {
		t.Fatalf("visible conversations = %d, want 0", len(values))
	}
}

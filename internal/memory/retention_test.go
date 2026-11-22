package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/conversation"
	"github.com/kimata1007/codex-runner-controller/internal/session"
)

func TestExpiredConversationHiddenFromUIAndCannotBeReopened(t *testing.T) {
	repository := NewRepository()
	value, err := repository.CreateConversation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = repository.CreateMessage(context.Background(), value.ID, conversation.NewMessage{Content: "preserve me"})
	if err != nil {
		t.Fatal(err)
	}
	repository.mu.Lock()
	repository.conversations[value.ID].UpdatedAt = time.Now().UTC().Add(-session.DefaultRetentionPeriod - time.Hour)
	repository.mu.Unlock()
	if values, err := repository.ListConversations(context.Background()); err != nil || len(values) != 0 {
		t.Fatalf("archived UI list = %+v, error = %v", values, err)
	}
	if _, err := repository.GetConversation(context.Background(), value.ID); !errors.Is(err, conversation.ErrArchived) {
		t.Fatalf("reopening expired conversation returned %v", err)
	}
	if _, _, err := repository.CreateMessage(context.Background(), value.ID, conversation.NewMessage{Content: "resurrect"}); !errors.Is(err, conversation.ErrArchived) {
		t.Fatalf("expired conversation accepted a new message: %v", err)
	}
	ids, err := repository.ArchiveInactiveConversations(context.Background(), time.Now().UTC().Add(-session.DefaultRetentionPeriod))
	if err != nil || len(ids) != 1 || ids[0] != value.ID {
		t.Fatalf("archive candidates=%v, error=%v", ids, err)
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	stored := repository.conversations[value.ID]
	if stored.Status != "archived" || len(stored.Messages) != 1 || stored.Messages[0].Content != "preserve me" {
		t.Fatalf("archiving lost history: %+v", stored)
	}
}

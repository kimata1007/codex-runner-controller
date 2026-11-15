package runnerapi

import (
	"context"
	"log"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/conversation"
	"github.com/kimata1007/codex-runner-controller/internal/session"
)

const (
	ConversationRetentionPeriod = session.DefaultRetentionPeriod
	conversationArchiveInterval = time.Hour
)

// RunConversationArchiver hides conversations that have been inactive for a
// week while leaving their messages, runs, and events in the database.
func RunConversationArchiver(ctx context.Context, store conversation.RetentionStore) {
	if store == nil {
		return
	}
	archive := func() {
		cutoff := time.Now().UTC().Add(-ConversationRetentionPeriod)
		if err := store.ArchiveInactiveConversations(ctx, cutoff); err != nil && ctx.Err() == nil {
			log.Printf("archive inactive conversations: %v", err)
		}
	}
	archive()
	ticker := time.NewTicker(conversationArchiveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			archive()
		}
	}
}

package runnerapi

import (
	"context"
	"log/slog"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/conversation"
	"github.com/kimata1007/codex-runner-controller/internal/session"
)

const conversationArchiveInterval = time.Hour

type ConversationSessionCleaner interface {
	DeleteSession(context.Context, string) error
}

// RunConversationArchiver persists archive state, then removes the owned
// Kubernetes session resources. Expired, archived IDs are returned on future
// sweeps, so cleanup errors can be retried after a restart.
func RunConversationArchiver(ctx context.Context, store conversation.RetentionStore, cleaner ConversationSessionCleaner) {
	if store == nil {
		return
	}
	sweep := func() {
		ids, err := store.ArchiveInactiveConversations(ctx, time.Now().UTC().Add(-session.DefaultRetentionPeriod))
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("failed to archive expired conversations", "error", err)
			}
			return
		}
		if cleaner == nil {
			return
		}
		for _, id := range ids {
			if err := cleaner.DeleteSession(ctx, id); err != nil && ctx.Err() == nil {
				slog.Error("failed to clean up archived conversation session", "error", err)
			}
		}
	}
	sweep()
	ticker := time.NewTicker(conversationArchiveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweep()
		}
	}
}

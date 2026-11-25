package runnerapi

import (
	"context"
	"errors"
	"log/slog"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/conversation"
	"github.com/kimata1007/codex-runner-controller/internal/logging"
	"github.com/kimata1007/codex-runner-controller/internal/runs"
	"github.com/kimata1007/codex-runner-controller/internal/session"
)

const defaultQueuePollInterval = 2 * time.Second

// RunQueueWorker drains durable queued messages after process startup and polls
// periodically so queued messages survive missed events and API restarts.
func (h *Handler) RunQueueWorker(ctx context.Context, interval time.Duration) {
	if h == nil || h.queueStore == nil || h.dispatcher == nil {
		return
	}
	if interval <= 0 {
		interval = defaultQueuePollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.processQueuedMessages(ctx)
		}
	}
}

func (h *Handler) processQueuedMessages(ctx context.Context) {
	if h.statusStore == nil {
		slog.ErrorContext(ctx, "message status store is not configured", "operation", "message_queue.claim")
		return
	}
	conversationIDs, err := h.queueStore.ListQueuedConversationIDs(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.ErrorContext(ctx, "failed to list queued messages", "operation", "message_queue.list", "error.type", logging.ErrorType(err))
		}
		return
	}
	for _, conversationID := range conversationIDs {
		if ctx.Err() != nil {
			return
		}
		message, claimed, err := h.queueStore.ClaimNextQueuedMessage(ctx, conversationID)
		if err != nil {
			slog.ErrorContext(ctx, "failed to claim queued message", "operation", "message_queue.claim", "conversation_id", conversationID, "error.type", logging.ErrorType(err))
			continue
		}
		if !claimed {
			continue
		}
		if _, err := h.startMessage(ctx, message, ""); err != nil {
			if errors.Is(err, session.ErrSessionHasActiveRun) {
				h.resetQueuedMessageID(ctx, message.ID)
				continue
			}
			_ = h.statusStore.SetMessageStatus(ctx, message.ID, "failed")
			if h.phase != nil {
				_ = h.phase.SetPhase(ctx, message.ConversationID, v1alpha1.PhaseFailed)
			}
			slog.ErrorContext(ctx, "failed to dispatch queued message", "operation", "message_queue.dispatch", "conversation_id", message.ConversationID, "message_id", message.ID, "error.type", logging.ErrorType(err))
		}
	}
}

func (h *Handler) startMessage(ctx context.Context, message conversation.Message, threadID string) (runs.Run, error) {
	conversationValue, err := h.repository.GetConversation(ctx, message.ConversationID)
	if err != nil {
		return runs.Run{}, err
	}
	if h.activator != nil {
		var repository *session.Repository
		if conversationValue.Repository != nil {
			repository = &session.Repository{URL: conversationValue.Repository.URL, Revision: conversationValue.Repository.Revision}
		}
		if err := h.activator.Activate(ctx, session.Activation{ConversationID: message.ConversationID, RequestedAt: time.Now().UTC(), Repository: repository}); err != nil {
			return runs.Run{}, err
		}
	}
	return h.dispatchClaimedMessage(ctx, message, threadID)
}

func (h *Handler) dispatchClaimedMessage(ctx context.Context, message conversation.Message, threadID string) (runs.Run, error) {
	if h.phase != nil {
		_ = h.phase.SetPhase(ctx, message.ConversationID, v1alpha1.PhaseBusy)
	}
	run, err := h.dispatcher.Dispatch(ctx, message.ConversationID, message.ID, threadID, message.Content)
	if err != nil {
		return runs.Run{}, err
	}
	return run, nil
}

func (h *Handler) resetQueuedMessageID(ctx context.Context, messageID string) {
	if h.statusStore != nil {
		_ = h.statusStore.SetMessageStatus(ctx, messageID, "queued")
	}
}

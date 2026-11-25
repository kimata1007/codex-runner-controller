package memory

import (
	"context"
	"testing"

	"github.com/kimata1007/codex-runner-controller/internal/conversation"
)

func TestQueueClaimsPreserveFIFO(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository()
	conv, err := repo.CreateConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := repo.CreateMessage(ctx, conv.ID, conversation.NewMessage{Content: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := repo.CreateMessage(ctx, conv.ID, conversation.NewMessage{Content: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := repo.ClaimQueuedMessage(ctx, second.ID); err != nil || claimed {
		t.Fatalf("later direct claim: claimed=%v err=%v", claimed, err)
	}
	if claimed, err := repo.ClaimQueuedMessage(ctx, first.ID); err != nil || !claimed {
		t.Fatalf("first direct claim: claimed=%v err=%v", claimed, err)
	}
	if _, claimed, err := repo.ClaimNextQueuedMessage(ctx, conv.ID); err != nil || claimed {
		t.Fatalf("worker while busy: claimed=%v err=%v", claimed, err)
	}
	if err := repo.SetMessageStatus(ctx, first.ID, "completed"); err != nil {
		t.Fatal(err)
	}
	next, claimed, err := repo.ClaimNextQueuedMessage(ctx, conv.ID)
	if err != nil || !claimed || next.ID != second.ID {
		t.Fatalf("next worker claim: message=%+v claimed=%v err=%v", next, claimed, err)
	}
}

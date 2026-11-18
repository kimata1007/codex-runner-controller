package runnerapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/kimata1007/codex-runner-controller/internal/conversation"
	"github.com/kimata1007/codex-runner-controller/internal/memory"
	"github.com/kimata1007/codex-runner-controller/internal/session"
)

func TestArchiveConversationPreservesHistory(t *testing.T) {
	repo := memory.NewRepository()
	value, err := repo.CreateConversation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.CreateMessage(context.Background(), value.ID, conversation.NewMessage{Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(NewHandler(HandlerDependencies{Repository: repo}))
	response := serveJSON(t, router, http.MethodDelete, "/v1/conversations/"+value.ID, "")
	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	archived, err := repo.GetConversation(context.Background(), value.ID)
	if err != nil || archived.Status != "archived" {
		t.Fatalf("conversation status=%q err=%v", archived.Status, err)
	}
	messages, err := repo.ListMessages(context.Background(), value.ID)
	if err != nil || len(messages) != 1 || messages[0].Content != "hello" {
		t.Fatalf("conversation history was not preserved: messages=%+v err=%v", messages, err)
	}
	if _, _, err := repo.CreateMessage(context.Background(), value.ID, conversation.NewMessage{Content: "continue"}); err != nil {
		t.Fatalf("could not resume archived conversation: %v", err)
	}
	resumed, err := repo.GetConversation(context.Background(), value.ID)
	if err != nil || resumed.Status != "active" {
		t.Fatalf("resumed conversation status=%q err=%v", resumed.Status, err)
	}
	if response := serveJSON(t, router, http.MethodDelete, "/v1/conversations/"+value.ID, ""); response.Code != http.StatusNoContent {
		t.Fatalf("repeated archive returned %d", response.Code)
	}
}

func TestArchiveConversationMissingReturnsNotFound(t *testing.T) {
	response := serveJSON(t, newTestRouter(), http.MethodDelete, "/v1/conversations/unknown", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d", response.Code)
	}
}

type failingSessionCleaner struct {
	err         error
	deleteCalls int
}

func (s *failingSessionCleaner) Activate(context.Context, session.Activation) error { return nil }
func (s *failingSessionCleaner) DeleteSession(context.Context, string) error {
	s.deleteCalls++
	return s.err
}

type failedArchiveRepository struct {
	*memory.Repository
	err   error
	calls int
}

func (r *failedArchiveRepository) ArchiveConversation(_ context.Context, _ string) error {
	r.calls++
	return r.err
}

func TestArchiveConversationSessionFailurePreservesHistory(t *testing.T) {
	repo := memory.NewRepository()
	conversationValue, err := repo.CreateConversation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cleaner := &failingSessionCleaner{err: errors.New("injected Kubernetes failure")}
	router := NewRouter(NewHandler(HandlerDependencies{Repository: repo, Activator: cleaner}))
	response := serveJSON(t, router, http.MethodDelete, "/v1/conversations/"+conversationValue.ID, "")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", response.Code)
	}
	if cleaner.deleteCalls != 1 {
		t.Fatalf("Kubernetes delete calls=%d", cleaner.deleteCalls)
	}
	if _, err := repo.GetConversation(context.Background(), conversationValue.ID); err != nil {
		t.Fatalf("history was lost: %v", err)
	}
}

func TestArchiveConversationDBFailureReturnsErrorAndKeepsHistory(t *testing.T) {
	base := memory.NewRepository()
	value, err := base.CreateConversation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	repo := &failedArchiveRepository{Repository: base, err: fmt.Errorf("injected archive failure")}
	router := NewRouter(NewHandler(HandlerDependencies{Repository: repo}))
	response := serveJSON(t, router, http.MethodDelete, "/v1/conversations/"+value.ID, "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d", response.Code)
	}
	if repo.calls != 1 {
		t.Fatalf("archive calls=%d", repo.calls)
	}
	if _, err := base.GetConversation(context.Background(), value.ID); err != nil {
		t.Fatalf("history was lost: %v", err)
	}
}

func TestArchiveConversationDoesNotDeleteUnknownSession(t *testing.T) {
	repo := memory.NewRepository()
	cleaner := &failingSessionCleaner{}
	router := NewRouter(NewHandler(HandlerDependencies{Repository: repo, Activator: cleaner}))
	response := serveJSON(t, router, http.MethodDelete, "/v1/conversations/nonexistent", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d", response.Code)
	}
	if cleaner.deleteCalls != 0 {
		t.Fatalf("unexpected session delete calls=%d", cleaner.deleteCalls)
	}
}

package runs

import (
	"context"
	"strings"
	"testing"
)

type fakeThreadClient struct {
	started   bool
	resumed   string
	turnID    string
	turnRunID string
	turnText  string
	startErr  error
	turnErr   error
}

func (f *fakeThreadClient) StartThread(context.Context) (string, error) {
	f.started = true
	return "thread-new", f.startErr
}

func (f *fakeThreadClient) ResumeThread(_ context.Context, threadID string) error {
	f.resumed = threadID
	return nil
}

func (f *fakeThreadClient) StartTurn(_ context.Context, threadID, runID, text string) error {
	f.turnID, f.turnRunID, f.turnText = threadID, runID, text
	return f.turnErr
}

type fakeRunStore struct {
	run      Run
	statuses []string
}

func (f *fakeRunStore) CreateRun(_ context.Context, conversationID, messageID, threadID string) (Run, error) {
	f.run = Run{ID: "run-1", ConversationID: conversationID, MessageID: messageID, ThreadID: threadID, Status: StatusQueued}
	return f.run, nil
}

func (f *fakeRunStore) SetRunStatus(_ context.Context, _ string, status, _, _ string) error {
	f.statuses = append(f.statuses, status)
	return nil
}

func TestMessageDispatcherStartsNewThreadAndSendsTurn(t *testing.T) {
	client := &fakeThreadClient{}
	store := &fakeRunStore{}
	run, err := NewMessageDispatcher(store, client).Dispatch(context.Background(), "conversation-1", "message-1", "", " hello ")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !client.started || client.resumed != "" {
		t.Fatalf("thread lifecycle = started:%v resumed:%q", client.started, client.resumed)
	}
	if run.ThreadID != "thread-new" || run.Status != StatusRunning {
		t.Fatalf("run = %+v", run)
	}
	if client.turnID != "thread-new" || client.turnRunID != "run-1" || client.turnText != "hello" {
		t.Fatalf("turn = %q %q %q", client.turnID, client.turnRunID, client.turnText)
	}
	if strings.Join(store.statuses, ",") != StatusRunning {
		t.Fatalf("statuses = %v", store.statuses)
	}
}

func TestMessageDispatcherResumesThreadAndMarksSendFailure(t *testing.T) {
	client := &fakeThreadClient{turnErr: context.Canceled}
	store := &fakeRunStore{}
	_, err := NewMessageDispatcher(store, client).Dispatch(context.Background(), "conversation-1", "message-1", "thread-existing", "hello")
	if err == nil || !strings.Contains(err.Error(), "send turn") {
		t.Fatalf("err = %v", err)
	}
	if client.started || client.resumed != "thread-existing" {
		t.Fatalf("thread lifecycle = started:%v resumed:%q", client.started, client.resumed)
	}
	if strings.Join(store.statuses, ",") != StatusRunning+","+StatusFailed {
		t.Fatalf("statuses = %v", store.statuses)
	}
}

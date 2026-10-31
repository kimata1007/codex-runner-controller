package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type approvalSender struct{ events chan AppServerRunEvent }

func (s approvalSender) Send(_ context.Context, event AppServerRunEvent) error {
	s.events <- event
	return nil
}

func TestApprovalManagerPublishesAndResolvesRequest(t *testing.T) {
	tracker := NewRunTracker()
	tracker.Set("run-1")
	sender := approvalSender{events: make(chan AppServerRunEvent, 1)}
	manager := NewApprovalManager(sender, tracker)
	id := int64(7)
	done := make(chan any, 1)
	go func() {
		result, err := manager.Handle(context.Background(), Message{ID: &id, Method: "item/commandExecution/requestApproval", Params: json.RawMessage(`{"command":"go test"}`)})
		if err != nil {
			done <- err
			return
		}
		done <- result
	}()
	select {
	case event := <-sender.events:
		if event.RunID != "run-1" || event.EventType != "approval/requested" {
			t.Fatalf("event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("approval event was not published")
	}
	if err := manager.Resolve("7", map[string]string{"decision": "accept"}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	select {
	case result := <-done:
		if result == nil {
			t.Fatal("result is nil")
		}
	case <-time.After(time.Second):
		t.Fatal("approval did not resolve")
	}
}

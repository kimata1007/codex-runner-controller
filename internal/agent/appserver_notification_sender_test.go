package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type orderedEventRecorder struct{ events chan AppServerRunEvent }

func (r orderedEventRecorder) Send(_ context.Context, event AppServerRunEvent) error {
	r.events <- event
	return nil
}

func TestQueuedRunEventSenderPreservesEnqueueOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	forwarded := make(chan AppServerRunEvent, 3)
	sender := NewQueuedRunEventSender(ctx, orderedEventRecorder{events: forwarded}, nil)
	for _, eventType := range []string{"item/started", "item/updated", "turn/completed"} {
		if err := sender.Send(context.Background(), AppServerRunEvent{RunID: "run-1", EventType: eventType}); err != nil {
			t.Fatalf("enqueue %s: %v", eventType, err)
		}
	}
	want := []string{"item/started", "item/updated", "turn/completed"}
	for i, eventType := range want {
		select {
		case event := <-forwarded:
			if event.EventType != eventType {
				t.Fatalf("event %d = %q, want %q", i, event.EventType, eventType)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for event %d (%s)", i, eventType)
		}
	}
}

func TestHTTPRunEventSenderSendsAppServerRunEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/runs/run-1/events" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer api-token" {
			t.Fatalf("authorization = %q", got)
		}
		var body struct {
			EventType string          `json:"eventType"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.EventType != "item/updated" || string(body.Payload) != `{"item":"done"}` {
			t.Fatalf("body = %+v", body)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	sender := NewHTTPRunEventSender(server.URL + "/internal/runs").WithBearerToken("api-token")
	if err := sender.Send(context.Background(), AppServerRunEvent{
		RunID:     "run-1",
		EventType: "item/updated",
		Payload:   json.RawMessage(`{"item":"done"}`),
	}); err != nil {
		t.Fatalf("report: %v", err)
	}
}

func TestHTTPRunEventSenderValidatesEvent(t *testing.T) {
	sender := NewHTTPRunEventSender("")
	if err := sender.Send(context.Background(), AppServerRunEvent{}); err == nil {
		t.Fatal("expected base URL error")
	}
	sender = NewHTTPRunEventSender("http://runner-api")
	if err := sender.Send(context.Background(), AppServerRunEvent{EventType: "item/updated"}); err == nil {
		t.Fatal("expected run ID error")
	}
}

func TestHTTPRunEventSenderAddsBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer api-token" {
			t.Fatalf("authorization = %q", got)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	sender := NewHTTPRunEventSender(server.URL).WithBearerToken(" api-token ")
	if err := sender.Send(context.Background(), AppServerRunEvent{RunID: "run-1", EventType: "turn/completed"}); err != nil {
		t.Fatalf("report: %v", err)
	}
}

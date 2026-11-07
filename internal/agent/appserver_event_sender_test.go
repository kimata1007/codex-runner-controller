package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPRunEventSenderSendsAppServerRunEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/runs/run-1/events" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
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

	sender := NewHTTPRunEventSender(server.URL + "/internal/runs")
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

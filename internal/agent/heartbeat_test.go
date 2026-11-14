package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPHeartbeatSinkReportsWithBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/sessions/conversation-1/heartbeat" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer agent-secret" {
			t.Fatalf("authorization = %q", got)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	sink, err := NewHTTPHeartbeatSinkWithToken(server.URL+"/internal/sessions", "conversation-1", "agent-secret")
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	if err := sink.WithHTTPClient(server.Client()).Report(context.Background()); err != nil {
		t.Fatalf("report: %v", err)
	}
}

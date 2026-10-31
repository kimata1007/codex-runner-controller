package agentclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kimata1007/codex-runner-controller/internal/agent"
)

func TestRunClientFactoryResolvesConversationEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/threads" {
			http.NotFound(w, r)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode thread request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"thread":{"id":"thread-1"}}`))
	}))
	defer server.Close()

	factory := NewRunClientFactory(StaticEndpointResolver{URL: server.URL}, agent.ThreadStartParams{})
	client, err := factory.ClientFor(context.Background(), "conversation-1")
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	threadID, err := client.StartThread(context.Background())
	if err != nil || threadID != "thread-1" {
		t.Fatalf("threadID = %q, err = %v", threadID, err)
	}
}

func TestStaticEndpointResolverRequiresConversationAndURL(t *testing.T) {
	if _, err := (StaticEndpointResolver{URL: "http://runner"}).Resolve(context.Background(), " "); err == nil {
		t.Fatal("expected missing conversation error")
	}
	if _, err := (StaticEndpointResolver{}).Resolve(context.Background(), "conversation-1"); err == nil {
		t.Fatal("expected missing URL error")
	}
}

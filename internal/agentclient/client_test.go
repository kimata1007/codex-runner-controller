package agentclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kimata1007/codex-runner-controller/internal/agent"
)

func TestClientCallsRunnerAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch r.URL.Path {
		case "/v1/threads":
			if payload["cwd"] != "/workspace" {
				t.Errorf("cwd = %v", payload["cwd"])
			}
			writeTestJSON(w, http.StatusCreated, map[string]string{"id": "thread-1"})
		case "/v1/threads/thread-1/resume":
			if _, exists := payload["threadId"]; exists {
				t.Errorf("threadId should come from the URL path, payload = %v", payload)
			}
			writeTestJSON(w, http.StatusOK, map[string]string{"id": "thread-1"})
		case "/v1/threads/thread-1/turns":
			if payload["text"] != "hello" {
				t.Errorf("text = %v", payload["text"])
			}
			if payload["runId"] != "run-1" {
				t.Errorf("runId = %v", payload["runId"])
			}
			writeTestJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL)
	thread, err := client.StartThread(context.Background(), agent.ThreadStartParams{CWD: "/workspace"})
	if err != nil || thread.ID != "thread-1" {
		t.Fatalf("start thread = %+v, err=%v", thread, err)
	}
	if _, err := client.ResumeThread(context.Background(), thread.ID, agent.ThreadStartParams{}); err != nil {
		t.Fatalf("resume thread: %v", err)
	}
	if err := client.StartTurnForRun(context.Background(), thread.ID, "run-1", []agent.TurnInput{{Type: "text", Text: "hello"}}); err != nil {
		t.Fatalf("start turn: %v", err)
	}
}

func TestClientReturnsRunnerAgentError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
	}))
	defer server.Close()

	_, err := NewClient(server.URL).StartThread(context.Background(), agent.ThreadStartParams{})
	if err == nil || !strings.Contains(err.Error(), "400 Bad Request") {
		t.Fatalf("err = %v", err)
	}
}

func writeTestJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		panic(err)
	}
}

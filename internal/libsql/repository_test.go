package libsql

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kimata1007/codex-runner-controller/internal/conversation"
)

func TestRepositoryScopesConversationQueriesToUser(t *testing.T) {
	server, queries, args := newRepositoryTestServer(t)
	defer server.Close()

	repository := NewRepository(NewClient(server.URL, "").WithHTTPClient(server.Client()), "user-1")

	if _, err := repository.GetConversation(context.Background(), "conversation-1"); err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if len(*queries) != 2 {
		t.Fatalf("get conversation issued %d queries, want 2", len(*queries))
	}

	if _, err := repository.ListMessages(context.Background(), "conversation-1"); err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(*queries) != 4 {
		t.Fatalf("list messages issued %d total queries, want 4", len(*queries))
	}

	if _, _, err := repository.CreateMessage(context.Background(), "conversation-1", conversation.NewMessage{Content: "hello"}); err != nil {
		t.Fatalf("create message: %v", err)
	}
	if len(*queries) != 6 {
		t.Fatalf("create message issued %d total queries, want 6", len(*queries))
	}

	for index, query := range *queries {
		if !strings.Contains(query, "user_id = ?") && !strings.Contains(query, "conversations.user_id = ?") {
			t.Errorf("query %d does not scope by user: %s", index+1, query)
		}
		if !containsTextValue((*args)[index], "user-1") {
			t.Errorf("query %d does not bind the repository user ID", index+1)
		}
	}
}

func TestRepositoryCreateFirstMessage(t *testing.T) {
	server, queries, args := newRepositoryTestServer(t)
	defer server.Close()

	repository := NewRepository(NewClient(server.URL, "").WithHTTPClient(server.Client()), "user-1")
	message, inserted, err := repository.CreateMessage(context.Background(), "conversation-1", conversation.NewMessage{Content: "hello"})
	if err != nil {
		t.Fatalf("create first message: %v", err)
	}
	if !inserted || message.Content != "hello" || message.ConversationID != "conversation-1" {
		t.Fatalf("unexpected first message: %+v, inserted=%v", message, inserted)
	}
	if len(*queries) != 2 {
		t.Fatalf("got %d queries, want insert and timestamp update", len(*queries))
	}
	insert := (*queries)[0]
	if !strings.Contains(insert, "COALESCE((SELECT MAX(sequence) FROM messages WHERE conversation_id = ?), 0) + 1") {
		t.Errorf("first message sequence is not initialized to 1: %s", insert)
	}
	if !strings.Contains(insert, "WHERE EXISTS (SELECT 1 FROM conversations WHERE id = ? AND user_id = ?)") {
		t.Errorf("insert is not scoped to the conversation owner: %s", insert)
	}
	if len((*args)[0]) != 8 || !containsTextValue((*args)[0], "user-1") {
		t.Errorf("unexpected insert arguments: %+v", (*args)[0])
	}
}

func newRepositoryTestServer(t *testing.T) (*httptest.Server, *[]string, *[][]Value) {
	t.Helper()
	queries := make([]string, 0, 8)
	args := make([][]Value, 0, 8)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body pipelineRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode pipeline request: %v", err)
			return
		}
		if len(body.Requests) != 1 || body.Requests[0].Stmt == nil {
			t.Errorf("unexpected pipeline request: %+v", body.Requests)
			return
		}

		statement := body.Requests[0].Stmt
		queries = append(queries, statement.SQL)
		args = append(args, statement.Args)

		result := statementResult{}
		switch {
		case strings.HasPrefix(statement.SQL, "SELECT c.id, c.status"):
			result.Rows = [][]Value{{Text("conversation-1"), Text("active"), Text("2026-01-01T00:00:00Z"), Text("2026-01-01T00:00:00Z"), Null(), Null()}}
		case strings.HasPrefix(statement.SQL, "SELECT id FROM conversations"):
			result.Rows = [][]Value{{Text("conversation-1")}}
		case strings.HasPrefix(statement.SQL, "INSERT INTO messages"):
			result.AffectedRowCount = 1
		}

		response, err := json.Marshal(executeResponse{
			Type:   "execute",
			Result: result,
		})
		if err != nil {
			t.Errorf("marshal execute response: %v", err)
			return
		}
		_ = json.NewEncoder(writer).Encode(pipelineResponse{
			Results: []streamResult{{Type: "ok", Response: response}},
		})
	}))
	return server, &queries, &args
}

func containsTextValue(values []Value, want string) bool {
	for _, value := range values {
		if value.Type == "text" && value.Value == want {
			return true
		}
	}
	return false
}

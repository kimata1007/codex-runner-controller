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

func TestRepositoryListsOnlyActiveRunsForOwnedUnarchivedConversation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body pipelineRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		sql := body.Requests[0].Stmt.SQL
		for _, required := range []string{"conversations.user_id = ?", "conversations.status != 'archived'", "runs.status IN ('running', 'waiting_approval')"} {
			if !strings.Contains(sql, required) {
				t.Errorf("query missing %q: %s", required, sql)
			}
		}
		data := json.RawMessage(`{"type":"execute","result":{"cols":[{"name":"id"},{"name":"status"}],"rows":[[{"type":"text","value":"run-1"},{"type":"text","value":"running"}]],"affected_row_count":0}}`)
		_ = json.NewEncoder(w).Encode(pipelineResponseWithClosedStream(streamResult{Type: "ok", Response: data}))
	}))
	defer server.Close()
	repository := NewRepository(NewClient(server.URL, "").WithHTTPClient(server.Client()), "user-1")
	runs, err := repository.ListActiveRuns(context.Background(), "conversation-1")
	if err != nil || len(runs) != 1 || runs[0].ID != "run-1" {
		t.Fatalf("active runs = %+v, error = %v", runs, err)
	}
}

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

func TestRepositoryListConversationsExcludesArchivedBeforeLimit(t *testing.T) {
	server, queries, args := newRepositoryTestServer(t)
	defer server.Close()

	repository := NewRepository(NewClient(server.URL, "").WithHTTPClient(server.Client()), "user-1")
	values, err := repository.ListConversations(context.Background())
	if err != nil {
		t.Fatalf("list conversations: %v", err)
	}
	if len(values) != 1 || values[0].ID != "conversation-1" {
		t.Fatalf("unexpected conversations: %+v", values)
	}
	if len(*queries) != 1 {
		t.Fatalf("list conversations issued %d queries, want 1", len(*queries))
	}
	query := (*queries)[0]
	if !strings.Contains(query, "WHERE c.user_id = ? AND c.status != 'archived'") {
		t.Errorf("list query must scope user and exclude archived: %s", query)
	}
	if !strings.Contains(query, "ORDER BY c.updated_at DESC\n\t\tLIMIT 100") {
		t.Errorf("archived filter must run before ordering and limit: %s", query)
	}
	if len((*args)[0]) != 1 || !containsTextValue((*args)[0], "user-1") {
		t.Errorf("unexpected owner filter arguments: %+v", (*args)[0])
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
		if len(body.Requests) != 2 || body.Requests[0].Stmt == nil || body.Requests[1].Type != "close" {
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
		_ = json.NewEncoder(writer).Encode(pipelineResponseWithClosedStream(streamResult{Type: "ok", Response: response}))
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

func TestRepositoryLatestThreadID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body pipelineRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if len(body.Requests) != 2 || body.Requests[0].Stmt == nil || body.Requests[1].Type != "close" || !strings.Contains(body.Requests[0].Stmt.SQL, "ORDER BY created_at DESC") {
			t.Errorf("unexpected query: %+v", body.Requests)
		}
		response := json.RawMessage(`{"type":"execute","result":{"cols":[{"name":"thread_id"}],"rows":[[{"type":"text","value":"thread-1"}]],"affected_row_count":0}}`)
		_ = json.NewEncoder(writer).Encode(pipelineResponseWithClosedStream(streamResult{Type: "ok", Response: response}))
	}))
	defer server.Close()

	repository := NewRepository(NewClient(server.URL, "").WithHTTPClient(server.Client()), "user-1")
	threadID, found, err := repository.LatestThreadID(context.Background(), "conversation-1")
	if err != nil {
		t.Fatalf("latest thread: %v", err)
	}
	if !found || threadID != "thread-1" {
		t.Fatalf("thread = %q, found=%v", threadID, found)
	}
}

func TestRepositoryListsPendingApprovalsForConversation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body pipelineRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if len(body.Requests) != 2 || body.Requests[1].Type != "close" {
			t.Errorf("stream was not closed: %+v", body.Requests)
			return
		}
		query := body.Requests[0].Stmt
		if !strings.Contains(query.SQL, "a.status = 'pending'") || !strings.Contains(query.SQL, "c.user_id = ?") {
			t.Errorf("query does not filter pending approvals by owner: %s", query.SQL)
		}
		response := json.RawMessage(`{"type":"execute","result":{"cols":[{"name":"run_id"},{"name":"request_id"},{"name":"action_type"},{"name":"summary"}],"rows":[[{"type":"text","value":"run-1"},{"type":"text","value":"17"},{"type":"text","value":"item/commandExecution/requestApproval"},{"type":"text","value":"{\"command\":\"go test ./...\"}"}]],"affected_row_count":0}}`)
		_ = json.NewEncoder(writer).Encode(pipelineResponseWithClosedStream(streamResult{Type: "ok", Response: response}))
	}))
	defer server.Close()

	repository := NewRepository(NewClient(server.URL, "").WithHTTPClient(server.Client()), "user-1")
	approvals, err := repository.ListPendingApprovals(context.Background(), "conversation-1")
	if err != nil {
		t.Fatalf("list pending approvals: %v", err)
	}
	if len(approvals) != 1 || approvals[0].RunID != "run-1" || approvals[0].RequestID != "17" || string(approvals[0].Params) != `{"command":"go test ./..."}` {
		t.Fatalf("approvals = %+v", approvals)
	}
}

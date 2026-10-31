package libsql

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kimata1007/codex-runner-controller/internal/httpapi"
)

func TestRepositoryCreateMessageUsesScalarSequenceSubquery(t *testing.T) {
	var statement statement
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body pipelineRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		statement = *body.Requests[0].Stmt
		_ = json.NewEncoder(writer).Encode(pipelineResponse{
			Results: []streamResult{{Type: "ok", Response: json.RawMessage(`{"type":"execute","result":{"cols":[],"rows":[],"affected_row_count":1}}`)}},
		})
	}))
	defer server.Close()

	repository := NewRepository(NewClient(server.URL, "").WithHTTPClient(server.Client()), "user-1")
	message, inserted, err := repository.CreateMessage("conversation-1", httpapi.MessageInput{Content: "hello"})
	if err != nil {
		t.Fatalf("create message: %v", err)
	}
	if !inserted || message.Content != "hello" {
		t.Fatalf("unexpected message result: %+v, inserted=%v", message, inserted)
	}
	if !strings.Contains(statement.SQL, "VALUES") || !strings.Contains(statement.SQL, "SELECT MAX(sequence)") {
		t.Fatalf("message insert does not support an empty conversation: %s", statement.SQL)
	}
	if len(statement.Args) != 6 {
		t.Fatalf("expected six SQL args, got %d", len(statement.Args))
	}
}

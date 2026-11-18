package libsql

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func batchSuccessResponse(size int, failAt int) pipelineResponse {
	results := make([]json.RawMessage, size)
	errors := make([]json.RawMessage, size)
	for i := 0; i < size; i++ {
		results[i] = json.RawMessage(`null`)
		errors[i] = json.RawMessage(`null`)
	}
	// BEGIN succeeds; each dependent statement succeeds up to any injected failure.
	for i := 0; i < size-2; i++ {
		if failAt >= 0 && i >= failAt {
			break
		}
		results[i] = json.RawMessage(`{"cols":[],"rows":[],"affected_row_count":1}`)
	}
	if failAt >= 0 {
		errors[failAt] = json.RawMessage(`{"message":"injected constraint violation"}`)
		// The final step must execute ROLLBACK.
		results[size-1] = json.RawMessage(`{"cols":[],"rows":[],"affected_row_count":0}`)
	} else {
		results[size-2] = json.RawMessage(`{"cols":[],"rows":[],"affected_row_count":0}`)
	}
	inner, _ := json.Marshal(map[string]any{"type": "batch", "result": map[string]any{"step_results": results, "step_errors": errors}})
	return pipelineResponse{Results: []streamResult{{Type: "ok", Response: inner}}}
}

func TestArchiveConversationPreservesHistoryAndScopesUser(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req pipelineRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		if len(req.Requests) != 2 || req.Requests[0].Stmt == nil || req.Requests[1].Type != "close" {
			t.Errorf("unexpected archive request: %+v", req.Requests)
			return
		}
		stmt := req.Requests[0].Stmt
		if !strings.Contains(stmt.SQL, "UPDATE conversations SET status = 'archived'") || !strings.Contains(stmt.SQL, "user_id = ?") {
			t.Errorf("archive is not an owner-scoped status update: %s", stmt.SQL)
		}
		if len(stmt.Args) != 3 || stmt.Args[1].Value != "conversation-1" || stmt.Args[2].Value != "user-1" {
			t.Errorf("incorrect archive args: %+v", stmt.Args)
		}
		result := json.RawMessage(`{"type":"execute","result":{"cols":[],"rows":[],"affected_row_count":1}}`)
		_ = json.NewEncoder(w).Encode(pipelineResponseWithClosedStream(streamResult{Type: "ok", Response: result}))
	}))
	defer server.Close()
	repo := NewRepository(NewClient(server.URL, "").WithHTTPClient(server.Client()), "user-1")
	if err := repo.ArchiveConversation(context.Background(), "conversation-1"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("request count=%d want 1", calls)
	}
}

func TestArchiveConversationUnknownOwnerReturnsNotFound(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		result := json.RawMessage(`{"type":"execute","result":{"cols":[],"rows":[],"affected_row_count":0}}`)
		_ = json.NewEncoder(w).Encode(pipelineResponseWithClosedStream(streamResult{Type: "ok", Response: result}))
	}))
	defer server.Close()
	repo := NewRepository(NewClient(server.URL, "").WithHTTPClient(server.Client()), "another-user")
	if err := repo.ArchiveConversation(context.Background(), "conversation-1"); err == nil {
		t.Fatal("missing conversation unexpectedly archived")
	}
	if calls != 1 {
		t.Fatalf("request count=%d want 1", calls)
	}
}

func TestExecTransactionRejectsUnconfirmedRollback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request pipelineRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		count := len(request.Requests[0].Batch.Steps)
		reply := batchSuccessResponse(count, 1)
		var batch batchResponse
		if err := json.Unmarshal(reply.Results[0].Response, &batch); err != nil {
			t.Error(err)
			return
		}
		batch.Result.StepResults[count-1] = json.RawMessage(`null`)
		encoded, _ := json.Marshal(batch)
		reply.Results[0].Response = encoded
		_ = json.NewEncoder(w).Encode(pipelineResponseWithClosedStream(reply.Results...))
	}))
	defer server.Close()
	client := NewClient(server.URL, "").WithHTTPClient(server.Client())
	err := client.ExecTransaction(context.Background(), []TxStatement{{SQL: "SELECT 1"}})
	if err == nil || !strings.Contains(err.Error(), "rollback not confirmed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecTransactionSerializesZeroStep(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request pipelineRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		steps := request.Requests[0].Batch.Steps
		if steps[1].Condition == nil || steps[1].Condition.Step == nil || *steps[1].Condition.Step != 0 {
			t.Errorf("zero-valued step was omitted: %+v", steps[1].Condition)
		}
		reply := batchSuccessResponse(len(steps), -1)
		_ = json.NewEncoder(w).Encode(pipelineResponseWithClosedStream(reply.Results...))
	}))
	defer server.Close()
	client := NewClient(server.URL, "").WithHTTPClient(server.Client())
	if err := client.ExecTransaction(context.Background(), []TxStatement{{SQL: "SELECT 1"}}); err != nil {
		t.Fatal(err)
	}
}

func TestExecTransactionRejectsMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(pipelineResponseWithClosedStream(streamResult{Type: "ok", Response: json.RawMessage(`{"type":"batch","result":{"step_results":[],"step_errors":[]}}`)}))
	}))
	defer server.Close()
	client := NewClient(server.URL, "").WithHTTPClient(server.Client())
	if err := client.ExecTransaction(context.Background(), []TxStatement{{SQL: "DELETE FROM messages"}}); err == nil {
		t.Fatal("invalid batch accepted as committed")
	}
}

func TestExecTransactionCanceledDoesNotSendRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++ }))
	defer server.Close()
	client := NewClient(server.URL, "").WithHTTPClient(server.Client())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.ExecTransaction(ctx, []TxStatement{{SQL: "DELETE FROM messages"}}); err == nil {
		t.Fatal("canceled transaction succeeded")
	}
	if requests != 0 {
		t.Fatalf("sent %d requests despite cancellation", requests)
	}
}

package libsql

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientExecUsesBatonAndAuthorization(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization header was not sent")
		}
		var body pipelineRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if requests == 2 && body.Baton == nil {
			t.Fatal("second request did not send baton")
		}
		_ = json.NewEncoder(writer).Encode(pipelineResponse{
			Baton:   func() *string { value := "baton-1"; return &value }(),
			Results: []streamResult{{Type: "ok", Response: json.RawMessage(`{"type":"execute","result":{"cols":[],"rows":[],"affected_row_count":1}}`)}},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token").WithHTTPClient(server.Client())
	if _, err := client.Exec(context.Background(), "SELECT 1", nil, true); err != nil {
		t.Fatalf("first exec: %v", err)
	}
	if _, err := client.Exec(context.Background(), "SELECT 1", nil, true); err != nil {
		t.Fatalf("second exec: %v", err)
	}
}

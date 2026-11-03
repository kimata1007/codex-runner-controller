package libsql

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestClientClearsBatonAfterHTTPError(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		var body pipelineRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch requests {
		case 1:
			if body.Baton != nil {
				t.Fatal("first request sent a baton")
			}
			_ = json.NewEncoder(writer).Encode(pipelineResponse{
				Baton:   func() *string { value := "expired-baton"; return &value }(),
				Results: []streamResult{{Type: "ok", Response: json.RawMessage(`{"type":"execute","result":{"cols":[],"rows":[],"affected_row_count":0}}`)}},
			})
		case 2:
			if body.Baton == nil || *body.Baton != "expired-baton" {
				t.Fatal("second request did not send the current baton")
			}
			http.Error(writer, "stream not found", http.StatusNotFound)
		case 3:
			if body.Baton != nil {
				t.Fatal("third request reused the invalid baton")
			}
			_ = json.NewEncoder(writer).Encode(pipelineResponse{
				Baton:   func() *string { value := "new-baton"; return &value }(),
				Results: []streamResult{{Type: "ok", Response: json.RawMessage(`{"type":"execute","result":{"cols":[],"rows":[],"affected_row_count":0}}`)}},
			})
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "").WithHTTPClient(server.Client())
	if _, err := client.Exec(context.Background(), "SELECT 1", nil, true); err != nil {
		t.Fatalf("first exec: %v", err)
	}
	if _, err := client.Exec(context.Background(), "SELECT 1", nil, true); err == nil {
		t.Fatal("second exec succeeded unexpectedly")
	}
	if _, err := client.Exec(context.Background(), "SELECT 1", nil, true); err != nil {
		t.Fatalf("third exec: %v", err)
	}
}

func TestValueMarshalsEmptyTextValue(t *testing.T) {
	encoded, err := json.Marshal(Text(""))
	if err != nil {
		t.Fatalf("marshal text value: %v", err)
	}
	if got := string(encoded); got != `{"type":"text","value":""}` {
		t.Fatalf("unexpected empty text JSON: %s", got)
	}

	encoded, err = json.Marshal(Null())
	if err != nil {
		t.Fatalf("marshal null value: %v", err)
	}
	if strings.Contains(string(encoded), `"value"`) {
		t.Fatalf("null value unexpectedly contains a value field: %s", encoded)
	}
}

package libsql

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestClientExecOpensAndClosesFreshStream(t *testing.T) {
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
		if body.Baton != nil {
			t.Fatalf("request %d sent a baton for a previous stream", requests)
		}
		if len(body.Requests) != 2 || body.Requests[1].Type != "close" {
			t.Fatalf("request %d did not close its stream: %+v", requests, body.Requests)
		}
		_ = json.NewEncoder(writer).Encode(pipelineResponse{
			Results: []streamResult{
				{Type: "ok", Response: json.RawMessage(`{"type":"execute","result":{"cols":[],"rows":[],"affected_row_count":1}}`)},
				{Type: "ok", Response: json.RawMessage(`{"type":"close"}`)},
			},
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

func TestClientDoesNotReuseStreamAfterHTTPError(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		var body pipelineRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Baton != nil {
			t.Fatalf("request %d sent a baton for a previous stream", requests)
		}
		if len(body.Requests) != 2 || body.Requests[1].Type != "close" {
			t.Fatalf("request %d did not close its stream: %+v", requests, body.Requests)
		}
		switch requests {
		case 1:
			_ = json.NewEncoder(writer).Encode(pipelineResponse{
				Results: []streamResult{
					{Type: "ok", Response: json.RawMessage(`{"type":"execute","result":{"cols":[],"rows":[],"affected_row_count":0}}`)},
					{Type: "ok", Response: json.RawMessage(`{"type":"close"}`)},
				},
			})
		case 2:
			http.Error(writer, "stream not found", http.StatusNotFound)
		case 3:
			_ = json.NewEncoder(writer).Encode(pipelineResponse{
				Results: []streamResult{
					{Type: "ok", Response: json.RawMessage(`{"type":"execute","result":{"cols":[],"rows":[],"affected_row_count":0}}`)},
					{Type: "ok", Response: json.RawMessage(`{"type":"close"}`)},
				},
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

func TestClientDoesNotReuseStreamAfterTransportError(t *testing.T) {
	var requests int
	client := NewClient("http://libsql", "").WithHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		var body pipelineRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch requests {
		case 1:
			if body.Baton != nil {
				t.Fatalf("request %d sent a baton for a previous stream", requests)
			}
			if len(body.Requests) != 2 || body.Requests[1].Type != "close" {
				t.Fatalf("request %d did not close its stream: %+v", requests, body.Requests)
			}
			return pipelineHTTPResponse(`{"baton":null,"results":[{"type":"ok","response":{"type":"execute","result":{"cols":[],"rows":[],"affected_row_count":0}}},{"type":"ok","response":{"type":"close"}}]}`), nil
		case 2:
			if body.Baton != nil {
				t.Fatalf("request %d sent a baton for a previous stream", requests)
			}
			if len(body.Requests) != 2 || body.Requests[1].Type != "close" {
				t.Fatalf("request %d did not close its stream: %+v", requests, body.Requests)
			}
			return nil, errors.New("connection reset")
		case 3:
			if body.Baton != nil {
				t.Fatalf("request %d sent a baton for a previous stream", requests)
			}
			if len(body.Requests) != 2 || body.Requests[1].Type != "close" {
				t.Fatalf("request %d did not close its stream: %+v", requests, body.Requests)
			}
			return pipelineHTTPResponse(`{"baton":null,"results":[{"type":"ok","response":{"type":"execute","result":{"cols":[],"rows":[],"affected_row_count":0}}},{"type":"ok","response":{"type":"close"}}]}`), nil
		default:
			t.Fatalf("unexpected request %d", requests)
			return nil, nil
		}
	})})
	for i := 0; i < 3; i++ {
		_, err := client.Exec(context.Background(), "SELECT 1", nil, true)
		if i == 1 && err == nil {
			t.Fatal("second request succeeded unexpectedly")
		}
		if i != 1 && err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
	}
}

func pipelineHTTPResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func pipelineResponseWithClosedStream(results ...streamResult) pipelineResponse {
	results = append(results, streamResult{Type: "ok", Response: json.RawMessage(`{"type":"close"}`)})
	return pipelineResponse{Results: results}
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

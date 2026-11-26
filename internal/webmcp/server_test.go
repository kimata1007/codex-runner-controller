package webmcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestServeExposesWebProxyToolsAndUsesAuthorizedProxyEndpoints(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		switch r.URL.Path {
		case "/v1/search":
			if r.URL.Query().Get("q") != "codex" {
				t.Errorf("search query = %q", r.URL.Query().Get("q"))
			}
		case "/v1/fetch":
			if r.URL.Query().Get("url") != "https://example.com/docs" {
				t.Errorf("fetch URL = %q", r.URL.Query().Get("url"))
			}
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
	})

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"web_search","arguments":{"query":"codex"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"web_fetch","arguments":{"url":"https://example.com/docs"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"web_fetch","arguments":{"url":"http://127.0.0.1/"}}}`,
	}, "\n") + "\n"
	var output strings.Builder
	client := &http.Client{Transport: transport}
	if err := serve(context.Background(), strings.NewReader(input), &output, "http://web-proxy", client); err != nil {
		t.Fatal(err)
	}
	var replies []map[string]json.RawMessage
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var reply map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &reply); err != nil {
			t.Fatal(err)
		}
		replies = append(replies, reply)
	}
	if len(replies) != 5 {
		t.Fatalf("reply count = %d", len(replies))
	}
	var rejected map[string]any
	if err := json.Unmarshal(replies[4]["error"], &rejected); err != nil || rejected == nil {
		t.Fatal("private and non-HTTPS URL was not rejected")
	}
}

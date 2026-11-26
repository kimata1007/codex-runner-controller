package githubmcp

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

func TestMCPToolsUseOnlyTheGitHubProxyOperations(t *testing.T) {
	seen := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen++
		if r.URL.Host != "github-proxy" {
			t.Errorf("unexpected host %q", r.URL.Host)
		}
		switch seen {
		case 1:
			if r.Method != http.MethodPost || r.URL.Path != "/v1/pulls" {
				t.Errorf("create request = %s %s", r.Method, r.URL.Path)
			}
			payload, _ := io.ReadAll(r.Body)
			var body map[string]string
			if json.Unmarshal(payload, &body) != nil || body["head"] != "codex/session-uid/fix" {
				t.Errorf("create payload = %s", payload)
			}
		case 2:
			if r.Method != http.MethodGet || r.URL.Path != "/v1/pulls/12/checks" {
				t.Errorf("checks request = %s %s", r.Method, r.URL.Path)
			}
		default:
			t.Errorf("unexpected request %d", seen)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"number":12}`))}, nil
	})}
	created, rpcErr := callTool(context.Background(), client, "http://github-proxy", "pull_request_create", json.RawMessage(`{"head":"codex/session-uid/fix","title":"Fix","body":"Details"}`))
	if rpcErr != nil || created == nil {
		t.Fatalf("create result = %#v, %v", created, rpcErr)
	}
	checked, rpcErr := callTool(context.Background(), client, "http://github-proxy", "pull_request_checks", json.RawMessage(`{"number":12}`))
	if rpcErr != nil || checked == nil || seen != 2 {
		t.Fatalf("checks result = %#v, %v, requests=%d", checked, rpcErr, seen)
	}
	if _, rpcErr := callTool(context.Background(), client, "http://github-proxy", "pull_request_status", json.RawMessage(`{"number":0}`)); rpcErr == nil {
		t.Fatal("accepted an invalid PR number")
	}
}

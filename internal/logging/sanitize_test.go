package logging

import (
	"strings"
	"testing"
)

func TestSanitizeErrorMessagePreservesUsefulInformation(t *testing.T) {
	tests := []struct {
		name string
		raw string
		want string
		hide []string
	}{
		{
			name: "JSON-RPC failure",
			raw: "start thread: jsonrpc -32601: method not handled",
			want: "method not handled",
		},
		{
			name: "GitHub authorization",
			raw: "GitHub API returned 403: Resource not accessible by integration",
			want: "Resource not accessible by integration",
		},
		{
			name: "auth, token, URL query and prompt",
			raw: "push failed: Authorization: Bearer abcdefghij012345; token=secret-token; prompt=\"the user's private question\"; url=https://example.test/action?access_token=abc123&prompt=secret",
			want: "push failed",
			hide: []string{"abcdefghij012345", "secret-token", "private question", "abc123", "access_token=abc123", "prompt=secret"},
		},
		{
			name: "JSON content",
			raw: `RPC: {"content":"private prompt", "message":"permission denied", "access_token":"abc123"}`,
			want: "permission denied",
			hide: []string{"private prompt", "abc123"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeErrorMessage(tt.raw)
			if !strings.Contains(got, tt.want) {
				t.Fatalf("lost useful information: %s", got)
			}
			for _, hidden := range tt.hide {
				if strings.Contains(got, hidden) {
					t.Errorf("leaked %q: %s", hidden, got)
				}
			}
		})
	}
}

func TestSanitizeHTTPPathKeepsIDsAndMasksTokens(t *testing.T) {
	if got := SanitizeHTTPPath("/v1/conversations/conversation-123/messages"); got != "/v1/conversations/conversation-123/messages" {
		t.Fatalf("ID was removed: %s", got)
	}
	if got := SanitizeHTTPPath("/v1/threads/ghp_abcdefghijk1234567/turns"); strings.Contains(got, "abcdefghijk") {
		t.Fatalf("token in path: %s", got)
	}
}

package agentclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"syscall"
	"testing"

	"github.com/kimata1007/codex-runner-controller/internal/agent"
)

type connectionRefusedTransport struct {
	attempts int
	seenBody string
	seenAuth string
}

func (t *connectionRefusedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.attempts++
	body, _ := io.ReadAll(r.Body)
	t.seenBody = string(body)
	t.seenAuth = r.Header.Get("Authorization")
	if t.attempts <= 2 {
		return nil, syscall.ECONNREFUSED
	}
	return &http.Response{
		StatusCode: http.StatusCreated,
		Body:       io.NopCloser(strings.NewReader(`{"id":"e2e-thread"}`)),
		Header:     make(http.Header),
	}, nil
}

func TestRetriesRefusedAgentConnectionsPreservingBodyAndToken(t *testing.T) {
	transport := &connectionRefusedTransport{}
	client := NewClientWithToken("http://agent.example", "test-token").
		WithHTTPClient(&http.Client{Transport: transport})
	thread, err := client.StartThread(context.Background(), agent.ThreadStartParams{Model: "test-model"})
	if err != nil {
		t.Fatalf("StartThread: %v", err)
	}
	if thread.ID != "e2e-thread" || transport.attempts != 3 {
		t.Fatalf("thread %+v, attempts %d", thread, transport.attempts)
	}
	if !strings.Contains(transport.seenBody, "test-model") {
		t.Fatalf("request payload not preserved on retry: %s", transport.seenBody)
	}
	if transport.seenAuth != "Bearer test-token" {
		t.Fatalf("authorization header missing after retry")
	}
}

type transportError struct{ count int }
func (t *transportError) RoundTrip(*http.Request) (*http.Response, error) {
	t.count++
	return nil, errors.New("connection reset after request")
}

func TestDoesNotRetryAmbiguousTransportErrors(t *testing.T) {
	transport := &transportError{}
	client := NewClient("http://agent.example").WithHTTPClient(&http.Client{Transport: transport})
	if _, err := client.StartThread(context.Background(), agent.ThreadStartParams{}); err == nil {
		t.Fatal("expected transport failure")
	}
	if transport.count != 1 {
		t.Fatalf("ambiguous errors must not be retried, attempts %d", transport.count)
	}
}

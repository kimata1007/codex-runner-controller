package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/kimata1007/codex-runner-controller/internal/telemetry"
	"net/url"
	"strings"
)

// HTTPHeartbeatSink reports that a session pod is still active. The heartbeat
// endpoint is separate from App Server events because a long-running tool or
// an approval wait may produce no JSON-RPC notification for a while.
type HTTPHeartbeatSink struct {
	url        string
	token      string
	httpClient *http.Client
}

func NewHTTPHeartbeatSink(baseURL, conversationID string) (*HTTPHeartbeatSink, error) {
	return NewHTTPHeartbeatSinkWithToken(baseURL, conversationID, "")
}

func NewHTTPHeartbeatSinkWithToken(baseURL, conversationID, token string) (*HTTPHeartbeatSink, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	conversationID = strings.TrimSpace(conversationID)
	if baseURL == "" || conversationID == "" {
		return nil, fmt.Errorf("heartbeat sink: base URL and conversation ID are required")
	}
	return &HTTPHeartbeatSink{
		url:        baseURL + "/" + url.PathEscape(conversationID) + "/heartbeat",
		token:      strings.TrimSpace(token),
		httpClient: &http.Client{Transport: telemetry.HTTPTransport(nil)},
	}, nil
}

func (s *HTTPHeartbeatSink) WithHTTPClient(client *http.Client) *HTTPHeartbeatSink {
	if client != nil {
		s.httpClient = client
	}
	return s
}

func (s *HTTPHeartbeatSink) Report(ctx context.Context) error {
	if s == nil || s.url == "" {
		return fmt.Errorf("heartbeat sink: URL is required")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, nil)
	if err != nil {
		return fmt.Errorf("heartbeat sink: create request: %w", err)
	}
	if s.token != "" {
		request.Header.Set("Authorization", "Bearer "+s.token)
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("heartbeat sink: send request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("heartbeat sink: runner api returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

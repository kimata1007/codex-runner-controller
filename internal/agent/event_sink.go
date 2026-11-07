package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// AppServerNotification is a server-initiated JSON-RPC notification from the
// Codex App Server.
type AppServerNotification struct {
	Method string
	Params json.RawMessage
}

// AppServerRunEvent is an App Server notification associated with one run and
// forwarded to the Runner API for persistence and streaming.
type AppServerRunEvent struct {
	RunID     string
	EventType string
	Payload   json.RawMessage
}

// RunEventSink forwards run events outside the session pod.
type RunEventSink interface {
	Report(context.Context, AppServerRunEvent) error
}

// HTTPRunEventSink posts run events to the Runner API's internal event endpoint.
// The endpoint is intentionally configured separately from the public message
// API because it is only reachable inside the cluster.
type HTTPRunEventSink struct {
	baseURL    string
	httpClient *http.Client
}

func NewHTTPRunEventSink(baseURL string) *HTTPRunEventSink {
	return &HTTPRunEventSink{baseURL: strings.TrimRight(baseURL, "/"), httpClient: http.DefaultClient}
}

func (s *HTTPRunEventSink) WithHTTPClient(client *http.Client) *HTTPRunEventSink {
	if client != nil {
		s.httpClient = client
	}
	return s
}

func (s *HTTPRunEventSink) Report(ctx context.Context, event AppServerRunEvent) error {
	if s == nil || s.baseURL == "" {
		return fmt.Errorf("event sink: base URL is required")
	}
	if strings.TrimSpace(event.RunID) == "" {
		return fmt.Errorf("event sink: run ID is required")
	}
	if strings.TrimSpace(event.EventType) == "" {
		return fmt.Errorf("event sink: event type is required")
	}
	if len(event.Payload) == 0 {
		event.Payload = json.RawMessage(`{}`)
	}
	payload, err := json.Marshal(struct {
		EventType string          `json:"eventType"`
		Payload   json.RawMessage `json:"payload"`
	}{EventType: event.EventType, Payload: event.Payload})
	if err != nil {
		return fmt.Errorf("event sink: encode payload: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.baseURL+"/"+url.PathEscape(event.RunID)+"/events", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("event sink: create request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := s.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("event sink: send request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("event sink: runner api returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

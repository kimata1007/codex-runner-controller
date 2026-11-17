package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/kimata1007/codex-runner-controller/internal/telemetry"
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

// RunEventSender sends run events to the Runner API.
type RunEventSender interface {
	Send(context.Context, AppServerRunEvent) error
}

// HTTPRunEventSender posts run events to the Runner API's internal event endpoint.
// The endpoint is intentionally configured separately from the public message
// API because it is only reachable inside the cluster.
type HTTPRunEventSender struct {
	baseURL     string
	bearerToken string
	httpClient  *http.Client
}

func NewHTTPRunEventSender(baseURL string) *HTTPRunEventSender {
	return &HTTPRunEventSender{baseURL: strings.TrimRight(baseURL, "/"), httpClient: &http.Client{Transport: telemetry.HTTPTransport(nil)}}
}

func (s *HTTPRunEventSender) WithHTTPClient(client *http.Client) *HTTPRunEventSender {
	if client != nil {
		s.httpClient = client
	}
	return s
}

func (s *HTTPRunEventSender) WithBearerToken(token string) *HTTPRunEventSender {
	if s != nil {
		s.bearerToken = strings.TrimSpace(token)
	}
	return s
}

func (s *HTTPRunEventSender) Send(ctx context.Context, event AppServerRunEvent) error {
	if s == nil || s.baseURL == "" {
		return fmt.Errorf("event sender: base URL is required")
	}
	if strings.TrimSpace(event.RunID) == "" {
		return fmt.Errorf("event sender: run ID is required")
	}
	if strings.TrimSpace(event.EventType) == "" {
		return fmt.Errorf("event sender: event type is required")
	}
	if len(event.Payload) == 0 {
		event.Payload = json.RawMessage(`{}`)
	}
	payload, err := json.Marshal(struct {
		EventType string          `json:"eventType"`
		Payload   json.RawMessage `json:"payload"`
	}{EventType: event.EventType, Payload: event.Payload})
	if err != nil {
		return fmt.Errorf("event sender: encode payload: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.baseURL+"/"+url.PathEscape(event.RunID)+"/events", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("event sender: create request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if s.bearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+s.bearerToken)
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("event sender: send request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("event sender: runner api returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

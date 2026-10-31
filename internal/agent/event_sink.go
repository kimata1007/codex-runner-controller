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

// AppEvent is the subset of an App Server notification that the Runner API
// needs to persist and stream to clients.
type AppEvent struct {
	RunID     string
	EventType string
	Payload   json.RawMessage
}

// EventSink forwards App Server notifications outside the session pod.
type EventSink interface {
	Report(context.Context, AppEvent) error
}

// HTTPEventSink posts events to the Runner API's internal event endpoint.
// The endpoint is intentionally configured separately from the public message
// API because it is only reachable inside the cluster.
type HTTPEventSink struct {
	baseURL    string
	httpClient *http.Client
}

func NewHTTPEventSink(baseURL string) *HTTPEventSink {
	return &HTTPEventSink{baseURL: strings.TrimRight(baseURL, "/"), httpClient: http.DefaultClient}
}

func (s *HTTPEventSink) WithHTTPClient(client *http.Client) *HTTPEventSink {
	if client != nil {
		s.httpClient = client
	}
	return s
}

func (s *HTTPEventSink) Report(ctx context.Context, event AppEvent) error {
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

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// RemoteClient calls the runner-agent HTTP API from Runner API or a controller.
// It keeps the App Server protocol behind the Pod-local HTTP boundary.
type RemoteClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewRemoteClient(baseURL string) *RemoteClient {
	return &RemoteClient{baseURL: strings.TrimRight(baseURL, "/"), httpClient: http.DefaultClient}
}

func (c *RemoteClient) WithHTTPClient(client *http.Client) *RemoteClient {
	if client != nil {
		c.httpClient = client
	}
	return c
}

func (c *RemoteClient) StartThread(ctx context.Context, params ThreadStartParams) (Thread, error) {
	var response threadStartResult
	if err := c.post(ctx, "/v1/threads", params, &response); err != nil {
		return Thread{}, fmt.Errorf("remote start thread: %w", err)
	}
	return response.Thread, nil
}

func (c *RemoteClient) ResumeThread(ctx context.Context, threadID string, params ThreadStartParams) (Thread, error) {
	if threadID == "" {
		return Thread{}, fmt.Errorf("remote resume thread: thread id is required")
	}
	request := struct {
		ThreadID string `json:"threadId"`
		ThreadStartParams
	}{ThreadID: threadID, ThreadStartParams: params}
	var response threadStartResult
	if err := c.post(ctx, "/v1/threads/"+threadID+"/resume", request, &response); err != nil {
		return Thread{}, fmt.Errorf("remote resume thread: %w", err)
	}
	return response.Thread, nil
}

func (c *RemoteClient) StartTurn(ctx context.Context, threadID string, input []TurnInput) error {
	if threadID == "" {
		return fmt.Errorf("remote start turn: thread id is required")
	}
	if len(input) == 0 {
		return fmt.Errorf("remote start turn: input is required")
	}
	text := ""
	for _, item := range input {
		if item.Type == "text" {
			text = item.Text
			break
		}
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("remote start turn: text input is required")
	}
	if err := c.post(ctx, "/v1/threads/"+threadID+"/turns", struct {
		Text string `json:"text"`
	}{Text: text}, nil); err != nil {
		return fmt.Errorf("remote start turn: %w", err)
	}
	return nil
}

func (c *RemoteClient) post(ctx context.Context, path string, requestBody any, responseBody any) error {
	if c == nil || c.baseURL == "" {
		return fmt.Errorf("remote client: base URL is required")
	}
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("agent returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	if responseBody == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(responseBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

var _ SessionAPI = (*RemoteClient)(nil)

package agentclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/kimata1007/codex-runner-controller/internal/agent"
)

// Client calls the runner-agent HTTP API from the Runner API.
// It keeps the App Server protocol behind the Pod-local HTTP boundary.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client { return NewClientWithToken(baseURL, "") }

func NewClientWithToken(baseURL, token string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, httpClient: http.DefaultClient}
}

func (c *Client) WithHTTPClient(client *http.Client) *Client {
	if client != nil {
		c.httpClient = client
	}
	return c
}

func (c *Client) StartThread(ctx context.Context, params agent.ThreadStartParams) (agent.Thread, error) {
	var response agent.Thread
	if err := c.post(ctx, "/v1/threads", params, &response); err != nil {
		return agent.Thread{}, fmt.Errorf("runner-agent start thread: %w", err)
	}
	return response, nil
}

func (c *Client) ResumeThread(ctx context.Context, threadID string, params agent.ThreadStartParams) (agent.Thread, error) {
	if threadID == "" {
		return agent.Thread{}, fmt.Errorf("runner-agent resume thread: thread id is required")
	}
	var response agent.Thread
	if err := c.post(ctx, "/v1/threads/"+threadID+"/resume", params, &response); err != nil {
		return agent.Thread{}, fmt.Errorf("runner-agent resume thread: %w", err)
	}
	return response, nil
}

func (c *Client) StartTurn(ctx context.Context, threadID string, input []agent.TurnInput) error {
	return c.startTurn(ctx, threadID, "", input)
}

// StartTurnForRun starts a turn and attaches the Runner API run ID to the
// request so asynchronous App Server notifications can be associated with it.
func (c *Client) StartTurnForRun(ctx context.Context, threadID, runID string, input []agent.TurnInput) error {
	return c.startTurn(ctx, threadID, runID, input)
}

func (c *Client) RespondApproval(ctx context.Context, requestID string, response any) error {
	if strings.TrimSpace(requestID) == "" {
		return fmt.Errorf("runner-agent respond approval: request id is required")
	}
	if err := c.post(ctx, "/v1/approvals/"+requestID, struct {
		Response any `json:"response"`
	}{Response: response}, nil); err != nil {
		return fmt.Errorf("runner-agent respond approval: %w", err)
	}
	return nil
}

func (c *Client) ReadAuthStatus(ctx context.Context) (agent.AccountStatus, error) {
	var response agent.AccountStatus
	if err := c.get(ctx, "/v1/auth/status", &response); err != nil {
		return agent.AccountStatus{}, fmt.Errorf("runner-agent read auth status: %w", err)
	}
	return response, nil
}

func (c *Client) StartChatGPTDeviceCodeLogin(ctx context.Context) (agent.DeviceCodeLogin, error) {
	var response agent.DeviceCodeLogin
	if err := c.post(ctx, "/v1/auth/login", struct{}{}, &response); err != nil {
		return agent.DeviceCodeLogin{}, fmt.Errorf("runner-agent start ChatGPT sign-in: %w", err)
	}
	return response, nil
}

func (c *Client) CancelChatGPTLogin(ctx context.Context, loginID string) error {
	if strings.TrimSpace(loginID) == "" {
		return fmt.Errorf("runner-agent cancel ChatGPT sign-in: login id is required")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/v1/auth/login/"+loginID, nil)
	if err != nil {
		return fmt.Errorf("runner-agent cancel ChatGPT sign-in: %w", err)
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("runner-agent returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *Client) startTurn(ctx context.Context, threadID, runID string, input []agent.TurnInput) error {
	if threadID == "" {
		return fmt.Errorf("runner-agent start turn: thread id is required")
	}
	if len(input) == 0 {
		return fmt.Errorf("runner-agent start turn: input is required")
	}
	text := ""
	for _, item := range input {
		if item.Type == "text" {
			text = item.Text
			break
		}
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("runner-agent start turn: text input is required")
	}
	if err := c.post(ctx, "/v1/threads/"+threadID+"/turns", struct {
		Text  string `json:"text"`
		RunID string `json:"runId,omitempty"`
	}{Text: text, RunID: runID}, nil); err != nil {
		return fmt.Errorf("runner-agent start turn: %w", err)
	}
	return nil
}

func (c *Client) post(ctx context.Context, path string, requestBody any, responseBody any) error {
	if c == nil || c.baseURL == "" {
		return fmt.Errorf("runner-agent client: base URL is required")
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
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("runner-agent returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	if responseBody == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(responseBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) get(ctx context.Context, path string, responseBody any) error {
	if c == nil || c.baseURL == "" {
		return fmt.Errorf("runner-agent client: base URL is required")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("runner-agent returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(response.Body).Decode(responseBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

var _ agent.SessionAPI = (*Client)(nil)

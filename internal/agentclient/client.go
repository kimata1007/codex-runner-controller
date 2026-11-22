package agentclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/agent"
	"github.com/kimata1007/codex-runner-controller/internal/telemetry"
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
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, httpClient: &http.Client{Transport: telemetry.HTTPTransport(nil)}}
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

func (c *Client) SetChatGPTAuthTokens(ctx context.Context, tokens agent.ChatGPTAuthTokens) error {
	if err := c.post(ctx, "/v1/auth/tokens", tokens, nil); err != nil {
		return fmt.Errorf("runner-agent set ChatGPT credentials: %w", err)
	}
	return nil
}

func (c *Client) StartDeviceLogin(ctx context.Context) (agent.DeviceLogin, error) {
	var result agent.DeviceLogin
	if err := c.post(ctx, "/v1/auth/device-login", struct{}{}, &result); err != nil {
		return agent.DeviceLogin{}, err
	}
	return result, nil
}

func (c *Client) ReadAccount(ctx context.Context, loginID string) (agent.AccountStatus, error) {
	var result agent.AccountStatus
	if strings.TrimSpace(loginID) == "" {
		loginID = "status"
	}
	if err := c.request(ctx, http.MethodGet, "/v1/auth/device-login/"+url.PathEscape(loginID), nil, &result); err != nil {
		return agent.AccountStatus{}, err
	}
	return result, nil
}

func (c *Client) CancelDeviceLogin(ctx context.Context, loginID string) error {
	return c.request(ctx, http.MethodDelete, "/v1/auth/device-login/"+url.PathEscape(loginID), nil, nil)
}

func (c *Client) ReadAuthCredentials(ctx context.Context) (json.RawMessage, error) {
	var result json.RawMessage
	if err := c.request(ctx, http.MethodGet, "/v1/auth/credentials", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) Logout(ctx context.Context) error {
	return c.request(ctx, http.MethodDelete, "/v1/auth/account", nil, nil)
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
	return c.request(ctx, http.MethodPost, path, requestBody, responseBody)
}

func (c *Client) request(ctx context.Context, method, path string, requestBody any, responseBody any) error {
	if c == nil || c.baseURL == "" {
		return fmt.Errorf("runner-agent client: base URL is required")
	}
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	// A newly Ready Agent Pod may precede the kube-proxy/EndpointSlice update.
	// Retry only ECONNREFUSED (the connection was never established); never
	// retry HTTP errors or ambiguous disconnects after a request was sent.
	const maxConnectionAttempts = 30
	const connectionRetryDelay = 200 * time.Millisecond
	var response *http.Response
	for attempt := 0; attempt < maxConnectionAttempts; attempt++ {
		request, requestErr := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
		if requestErr != nil {
			return fmt.Errorf("create request: %w", requestErr)
		}
		request.Header.Set("Content-Type", "application/json")
		if c.token != "" {
			request.Header.Set("Authorization", "Bearer "+c.token)
		}
		response, err = c.httpClient.Do(request)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.ECONNREFUSED) || attempt == maxConnectionAttempts-1 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(connectionRetryDelay):
		}
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

var _ agent.SessionAPI = (*Client)(nil)

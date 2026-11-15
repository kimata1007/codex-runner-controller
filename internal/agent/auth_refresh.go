package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ChatGPTAuthTokenRefresher forwards App Server refresh requests to the Runner API.
type ChatGPTAuthTokenRefresher struct {
	URL        string
	Bearer     string
	HTTPClient *http.Client
}

func (r ChatGPTAuthTokenRefresher) Handle(ctx context.Context, message Message) (any, error) {
	if message.Method != "account/chatgptAuthTokens/refresh" {
		return nil, errRequestNotHandled
	}
	if strings.TrimSpace(r.URL) == "" {
		return nil, fmt.Errorf("ChatGPT auth refresh endpoint is not configured")
	}
	var params struct {
		PreviousAccountID string `json:"previousAccountId"`
	}
	if err := json.Unmarshal(message.Params, &params); err != nil || params.PreviousAccountID == "" {
		return nil, fmt.Errorf("invalid ChatGPT auth refresh request")
	}
	payload, _ := json.Marshal(map[string]string{"previousAccountId": params.PreviousAccountID})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.URL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if r.Bearer != "" {
		request.Header.Set("Authorization", "Bearer "+r.Bearer)
	}
	client := r.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("auth broker returned %s", response.Status)
	}
	var result ChatGPTAuthTokens
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode refreshed ChatGPT credentials: %w", err)
	}
	if result.AccessToken == "" || result.AccountID != params.PreviousAccountID {
		return nil, fmt.Errorf("auth broker returned credentials for a different account")
	}
	return result, nil
}

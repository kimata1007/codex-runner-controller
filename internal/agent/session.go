package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// ClientInfo identifies runner-agent to Codex App Server.
type ClientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

// Thread contains the durable identity returned by App Server.
type Thread struct {
	ID string `json:"id"`
}

// ThreadStartParams are the options used when creating a new thread.
// Additional App Server options can be added without changing the lifecycle API.
type ThreadStartParams struct {
	Model          string `json:"model,omitempty"`
	CWD            string `json:"cwd,omitempty"`
	ApprovalPolicy string `json:"approvalPolicy,omitempty"`
}

type threadStartResult struct {
	Thread Thread `json:"thread"`
}

// TurnInput is a user input item sent to turn/start.
type TurnInput struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// TurnStartParams contains one user turn for an existing thread.
type TurnStartParams struct {
	ThreadID string      `json:"threadId"`
	Input    []TurnInput `json:"input"`
}

// AccountStatus describes whether App Server needs an OpenAI sign-in.
type AccountStatus struct {
	Account            *AccountInfo `json:"account"`
	RequiresOpenAIAuth bool         `json:"requiresOpenaiAuth"`
}

type AccountInfo struct {
	Type     string `json:"type,omitempty"`
	Email    string `json:"email,omitempty"`
	PlanType string `json:"planType,omitempty"`
}

// DeviceCodeLogin is the remote-friendly ChatGPT sign-in challenge returned by App Server.
type DeviceCodeLogin struct {
	LoginID         string `json:"loginId"`
	VerificationURL string `json:"verificationUrl"`
	UserCode        string `json:"userCode"`
}

type AccountClient interface {
	ReadAccountStatus(context.Context) (AccountStatus, error)
	StartChatGPTDeviceCodeLogin(context.Context) (DeviceCodeLogin, error)
	CancelChatGPTLogin(context.Context, string) error
}

// Session owns the App Server lifecycle for one CodexSession.
// It deliberately does not persist thread state: the controller/API persists
// the thread ID, while this object only manages the live child process.
type Session struct {
	client       *JSONRPCClient
	loginMu      sync.Mutex
	pendingLogin *DeviceCodeLogin
}

func NewSession(client *JSONRPCClient) *Session {
	return &Session{client: client}
}

func (s *Session) Initialize(ctx context.Context, info ClientInfo) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("agent session: nil client")
	}
	var result map[string]any
	if err := s.client.Call(ctx, "initialize", map[string]any{"clientInfo": info}, &result); err != nil {
		return fmt.Errorf("initialize app server: %w", err)
	}
	if err := s.client.Notify("initialized", map[string]any{}); err != nil {
		return fmt.Errorf("notify app server initialized: %w", err)
	}
	return nil
}

func (s *Session) StartThread(ctx context.Context, params ThreadStartParams) (Thread, error) {
	if s == nil || s.client == nil {
		return Thread{}, fmt.Errorf("agent session: nil client")
	}
	var result threadStartResult
	if err := s.client.Call(ctx, "thread/start", params, &result); err != nil {
		return Thread{}, fmt.Errorf("start thread: %w", err)
	}
	if result.Thread.ID == "" {
		return Thread{}, fmt.Errorf("start thread: response did not contain thread id")
	}
	return result.Thread, nil
}

func (s *Session) ResumeThread(ctx context.Context, threadID string, params ThreadStartParams) (Thread, error) {
	if s == nil || s.client == nil {
		return Thread{}, fmt.Errorf("agent session: nil client")
	}
	if threadID == "" {
		return Thread{}, fmt.Errorf("resume thread: thread id is required")
	}
	request := struct {
		ThreadID string `json:"threadId"`
		ThreadStartParams
	}{ThreadID: threadID, ThreadStartParams: params}
	var result threadStartResult
	if err := s.client.Call(ctx, "thread/resume", request, &result); err != nil {
		return Thread{}, fmt.Errorf("resume thread %q: %w", threadID, err)
	}
	if result.Thread.ID == "" {
		return Thread{}, fmt.Errorf("resume thread: response did not contain thread id")
	}
	return result.Thread, nil
}

func (s *Session) StartTurn(ctx context.Context, threadID string, input []TurnInput) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("agent session: nil client")
	}
	if threadID == "" {
		return fmt.Errorf("start turn: thread id is required")
	}
	if len(input) == 0 {
		return fmt.Errorf("start turn: input is required")
	}
	var result map[string]any
	if err := s.client.Call(ctx, "turn/start", TurnStartParams{ThreadID: threadID, Input: input}, &result); err != nil {
		return fmt.Errorf("start turn for thread %q: %w", threadID, err)
	}
	return nil
}

func (s *Session) ReadAccountStatus(ctx context.Context) (AccountStatus, error) {
	if s == nil || s.client == nil {
		return AccountStatus{}, fmt.Errorf("read account status: nil client")
	}
	var result AccountStatus
	if err := s.client.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &result); err != nil {
		return AccountStatus{}, fmt.Errorf("read account status: %w", err)
	}
	if !result.RequiresOpenAIAuth {
		s.loginMu.Lock()
		s.pendingLogin = nil
		s.loginMu.Unlock()
	}
	return result, nil
}

func (s *Session) StartChatGPTDeviceCodeLogin(ctx context.Context) (DeviceCodeLogin, error) {
	if s == nil || s.client == nil {
		return DeviceCodeLogin{}, fmt.Errorf("start ChatGPT sign-in: nil client")
	}
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	if s.pendingLogin != nil {
		return *s.pendingLogin, nil
	}
	var result DeviceCodeLogin
	if err := s.client.Call(ctx, "account/login/start", map[string]string{"type": "chatgptDeviceCode"}, &result); err != nil {
		return DeviceCodeLogin{}, fmt.Errorf("start ChatGPT sign-in: %w", err)
	}
	if result.LoginID == "" || result.VerificationURL == "" || result.UserCode == "" {
		return DeviceCodeLogin{}, fmt.Errorf("start ChatGPT sign-in: incomplete device code response")
	}
	s.pendingLogin = &result
	return result, nil
}

func (s *Session) CancelChatGPTLogin(ctx context.Context, loginID string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("cancel ChatGPT sign-in: nil client")
	}
	if strings.TrimSpace(loginID) == "" {
		return fmt.Errorf("cancel ChatGPT sign-in: login id is required")
	}
	if err := s.client.Call(ctx, "account/login/cancel", map[string]string{"loginId": loginID}, nil); err != nil {
		return err
	}
	s.loginMu.Lock()
	if s.pendingLogin != nil && s.pendingLogin.LoginID == loginID {
		s.pendingLogin = nil
	}
	s.loginMu.Unlock()
	return nil
}

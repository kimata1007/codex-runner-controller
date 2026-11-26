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
const kubernetesSandboxMode = "danger-full-access"

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

// ChatGPTAuthTokens are short-lived credentials supplied by the host application.
// The refresh token stays in the Runner API auth broker.
type ChatGPTAuthTokens struct {
	AccessToken string `json:"accessToken"`
	AccountID   string `json:"chatgptAccountId"`
	PlanType    string `json:"chatgptPlanType,omitempty"`
}

type AuthTokenClient interface {
	SetChatGPTAuthTokens(context.Context, ChatGPTAuthTokens) error
}

type DeviceLogin struct {
	LoginID         string `json:"loginId"`
	VerificationURL string `json:"verificationUrl"`
	UserCode        string `json:"userCode"`
}

type AccountStatus struct {
	Account *struct {
		Type     string `json:"type"`
		Email    string `json:"email"`
		PlanType string `json:"planType"`
	} `json:"account"`
}

// Session owns the App Server lifecycle for one CodexSession.
// It deliberately does not persist thread state: the controller/API persists
// the thread ID, while this object only manages the live child process.
type Session struct {
	client              *JSONRPCClient
	loginMu             sync.Mutex
	activeAuthAccountID string
}

func NewSession(client *JSONRPCClient) *Session {
	return &Session{client: client}
}

func (s *Session) Initialize(ctx context.Context, info ClientInfo) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("agent session: nil client")
	}
	var result map[string]any
	if err := s.client.Call(ctx, "initialize", map[string]any{
		"clientInfo":   info,
		"capabilities": map[string]bool{"experimentalApi": true},
	}, &result); err != nil {
		return fmt.Errorf("initialize app server: %w", err)
	}
	if err := s.client.Notify("initialized", map[string]any{}); err != nil {
		return fmt.Errorf("notify app server initialized: %w", err)
	}
	return nil
}

func (s *Session) LoginWithChatGPTAuthTokens(ctx context.Context, tokens ChatGPTAuthTokens) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("set ChatGPT credentials: nil client")
	}
	if strings.TrimSpace(tokens.AccessToken) == "" || strings.TrimSpace(tokens.AccountID) == "" {
		return fmt.Errorf("set ChatGPT credentials: access token and account id are required")
	}
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	if s.activeAuthAccountID == tokens.AccountID {
		return nil
	}
	var result map[string]any
	if err := s.client.Call(ctx, "account/login/start", map[string]any{
		"type":             "chatgptAuthTokens",
		"accessToken":      tokens.AccessToken,
		"chatgptAccountId": tokens.AccountID,
		"chatgptPlanType":  tokens.PlanType,
	}, &result); err != nil {
		return fmt.Errorf("set ChatGPT credentials: %w", err)
	}
	s.activeAuthAccountID = tokens.AccountID
	return nil
}

func (s *Session) SetChatGPTAuthTokens(ctx context.Context, tokens ChatGPTAuthTokens) error {
	return s.LoginWithChatGPTAuthTokens(ctx, tokens)
}

func (s *Session) StartDeviceLogin(ctx context.Context) (DeviceLogin, error) {
	var result DeviceLogin
	if err := s.client.Call(ctx, "account/login/start", map[string]string{"type": "chatgptDeviceCode"}, &result); err != nil {
		return DeviceLogin{}, fmt.Errorf("start ChatGPT device login: %w", err)
	}
	if result.LoginID == "" || result.VerificationURL == "" || result.UserCode == "" {
		return DeviceLogin{}, fmt.Errorf("start ChatGPT device login: incomplete response")
	}
	return result, nil
}

func (s *Session) ReadAccount(ctx context.Context) (AccountStatus, error) {
	var result AccountStatus
	if err := s.client.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &result); err != nil {
		return AccountStatus{}, fmt.Errorf("read ChatGPT account: %w", err)
	}
	return result, nil
}

func (s *Session) CancelDeviceLogin(ctx context.Context, loginID string) error {
	return s.client.Call(ctx, "account/login/cancel", map[string]string{"loginId": loginID}, nil)
}

func (s *Session) Logout(ctx context.Context) error {
	return s.client.Call(ctx, "account/logout", map[string]any{}, nil)
}

func (s *Session) SetRequestHandler(handler RequestHandler) {
	if s != nil && s.client != nil {
		s.client.SetRequestHandler(handler)
	}
}

func (s *Session) StartThread(ctx context.Context, params ThreadStartParams) (Thread, error) {
	if s == nil || s.client == nil {
		return Thread{}, fmt.Errorf("agent session: nil client")
	}
	var result threadStartResult
	request := struct {
		ThreadStartParams
		Sandbox string `json:"sandbox"`
	}{params, kubernetesSandboxMode}
	if err := s.client.Call(ctx, "thread/start", request, &result); err != nil {
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
		Sandbox string `json:"sandbox"`
	}{ThreadID: threadID, ThreadStartParams: params, Sandbox: kubernetesSandboxMode}
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
	request := struct {
		TurnStartParams
		SandboxPolicy struct {
			Type string `json:"type"`
		} `json:"sandboxPolicy"`
	}{TurnStartParams: TurnStartParams{ThreadID: threadID, Input: input}}
	request.SandboxPolicy.Type = "dangerFullAccess"
	if err := s.client.Call(ctx, "turn/start", request, &result); err != nil {
		return fmt.Errorf("start turn for thread %q: %w", threadID, err)
	}
	return nil
}

func (s *Session) InterruptTurn(ctx context.Context, threadID, turnID string) error {
	if s == nil || s.client == nil || threadID == "" || turnID == "" {
		return fmt.Errorf("interrupt turn: client, thread and turn are required")
	}
	return s.client.Call(ctx, "turn/interrupt", map[string]string{"threadId": threadID, "turnId": turnID}, nil)
}

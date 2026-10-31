package agent

import (
	"context"
	"fmt"
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

// Session owns the App Server lifecycle for one CodexSession.
// It deliberately does not persist thread state: the controller/API persists
// the thread ID, while this object only manages the live child process.
type Session struct {
	client *JSONRPCClient
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

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
)

type ApprovalManager struct {
	mu      sync.Mutex
	pending map[string]*pendingApproval
	sender  RunEventSender
	tracker *RunTracker
}

type pendingApproval struct {
	runID    string
	decision chan any
}

func NewApprovalManager(sender RunEventSender, tracker *RunTracker) *ApprovalManager {
	return &ApprovalManager{pending: make(map[string]*pendingApproval), sender: sender, tracker: tracker}
}

func (m *ApprovalManager) Handle(ctx context.Context, message Message) (any, error) {
	if !isApprovalRequest(message.Method) {
		return nil, errRequestNotHandled
	}
	if m == nil {
		return nil, fmt.Errorf("approval manager is not configured")
	}
	if message.ID == nil {
		return nil, fmt.Errorf("approval request id is required")
	}
	requestID := strconv.FormatInt(*message.ID, 10)
	pending := &pendingApproval{runID: m.tracker.Current(), decision: make(chan any, 1)}
	m.mu.Lock()
	m.pending[requestID] = pending
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		if m.pending[requestID] == pending {
			delete(m.pending, requestID)
		}
		m.mu.Unlock()
	}()

	if m.sender != nil && pending.runID != "" {
		payload, _ := json.Marshal(struct {
			RequestID string          `json:"requestId"`
			Method    string          `json:"method"`
			Params    json.RawMessage `json:"params"`
		}{RequestID: requestID, Method: message.Method, Params: message.Params})
		_ = m.sender.Send(context.Background(), AppServerRunEvent{RunID: pending.runID, EventType: "approval/requested", Payload: payload})
	}
	select {
	case response := <-pending.decision:
		return response, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *ApprovalManager) Resolve(requestID string, response any) error {
	return m.resolve("", requestID, response)
}

func (m *ApprovalManager) ResolveForRun(runID, requestID string, response any) error {
	if runID == "" {
		return fmt.Errorf("approval run id is required")
	}
	return m.resolve(runID, requestID, response)
}

func (m *ApprovalManager) resolve(runID, requestID string, response any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	pending := m.pending[requestID]
	if pending == nil || (runID != "" && pending.runID != runID) {
		return fmt.Errorf("approval request %q not found", requestID)
	}
	delete(m.pending, requestID)
	pending.decision <- response
	return nil
}

func isApprovalRequest(method string) bool {
	switch method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/permissions/requestApproval", "item/tool/requestUserInput", "mcpServer/elicitation/request":
		return true
	default:
		return false
	}
}

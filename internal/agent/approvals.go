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
	pending map[string]chan any
	sender  RunEventSender
	tracker *RunTracker
}

func NewApprovalManager(sender RunEventSender, tracker *RunTracker) *ApprovalManager {
	return &ApprovalManager{pending: make(map[string]chan any), sender: sender, tracker: tracker}
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
	decision := make(chan any, 1)
	m.mu.Lock()
	m.pending[requestID] = decision
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.pending, requestID)
		m.mu.Unlock()
	}()

	if m.sender != nil && m.tracker != nil && m.tracker.Current() != "" {
		runID := m.tracker.Current()
		payload, _ := json.Marshal(struct {
			RequestID string          `json:"requestId"`
			Method    string          `json:"method"`
			Params    json.RawMessage `json:"params"`
		}{RequestID: requestID, Method: message.Method, Params: message.Params})
		go func() {
			_ = m.sender.Send(context.Background(), AppServerRunEvent{RunID: runID, EventType: "approval/requested", Payload: payload})
		}()
	}
	select {
	case response := <-decision:
		return response, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *ApprovalManager) Resolve(requestID string, response any) error {
	m.mu.Lock()
	decision, ok := m.pending[requestID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("approval request %q not found", requestID)
	}
	decision <- response
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

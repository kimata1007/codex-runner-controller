package runs

import (
	"context"
	"encoding/json"
	"strings"
)

const (
	ApprovalPending  = "pending"
	ApprovalApproved = "approved"
	ApprovalRejected = "rejected"
	ApprovalExpired  = "expired"
)

// ApprovalStore persists the approval audit record separately from the
// in-memory JSON-RPC waiter in runner-agent.
type ApprovalStore interface {
	RecordApproval(context.Context, string, string, string, string) error
	ResolveApproval(context.Context, string, string, string, any) error
}

// ApprovalStatus maps the common client decisions to the durable status enum.
// Unknown response shapes are treated as approved only after the agent has
// accepted and forwarded the protocol response.
func ApprovalStatus(response any) string {
	payload, err := json.Marshal(response)
	if err == nil {
		value := strings.ToLower(string(payload))
		if strings.Contains(value, "reject") || strings.Contains(value, "decline") || strings.Contains(value, "deny") {
			return ApprovalRejected
		}
	}
	return ApprovalApproved
}

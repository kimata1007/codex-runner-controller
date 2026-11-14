package runs

import "testing"

func TestApprovalStatus(t *testing.T) {
	if got := ApprovalStatus(map[string]string{"decision": "accept"}); got != ApprovalApproved {
		t.Fatalf("accept status = %q", got)
	}
	if got := ApprovalStatus(map[string]string{"decision": "decline"}); got != ApprovalRejected {
		t.Fatalf("decline status = %q", got)
	}
}

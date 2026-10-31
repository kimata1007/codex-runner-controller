package runs

import "testing"

func TestRunStatusesMatchSchema(t *testing.T) {
	statuses := []string{StatusQueued, StatusRunning, StatusWaitingApproval, StatusCompleted, StatusFailed, StatusInterrupted}
	if len(statuses) != 6 {
		t.Fatalf("status count = %d", len(statuses))
	}
	for _, status := range statuses {
		if status == "" {
			t.Fatal("status must not be empty")
		}
	}
}

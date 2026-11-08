package runs

import "testing"

func TestStatusForEvent(t *testing.T) {
	cases := []struct {
		eventType, status string
		ok                bool
	}{
		{"turn/completed", StatusCompleted, true},
		{"turn/failed", StatusFailed, true},
		{"turn/interrupted", StatusInterrupted, true},
		{"approval/requested", StatusWaitingApproval, true},
		{"item/updated", "", false},
	}
	for _, test := range cases {
		status, ok := StatusForEvent(test.eventType)
		if status != test.status || ok != test.ok {
			t.Errorf("StatusForEvent(%q) = %q, %v", test.eventType, status, ok)
		}
	}
}

package runs

import "testing"

func TestStatusForEvent(t *testing.T) {
	cases := []struct {
		eventType, payload, status string
		ok                         bool
	}{
		{"turn/completed", `{"threadId":"thread-1","turn":{"status":"completed"}}`, StatusCompleted, true},
		{"turn/completed", `{"turn":{"status":"failed"}}`, StatusFailed, true},
		{"turn/completed", `{"turn":{"status":"interrupted"}}`, StatusInterrupted, true},
		{"turn/completed", `{"turn":{"status":"inProgress"}}`, "", false},
		{"turn/completed", `{}`, "", false},
		{"item/updated", `{}`, "", false},
		{"approval/requested", `{}`, "", false},
	}
	for _, test := range cases {
		status, ok := StatusForEvent(test.eventType, test.payload)
		if status != test.status || ok != test.ok {
			t.Errorf("StatusForEvent(%q, %s) = %q, %v", test.eventType, test.payload, status, ok)
		}
	}
}

func TestErrorMessageForEvent(t *testing.T) {
	got := ErrorMessageForEvent("turn/completed", `{"turn":{"status":"failed","error":{"message":"model error"}}}`)
	if got != "model error" {
		t.Fatalf("ErrorMessageForEvent() = %q, want %q", got, "model error")
	}
}

package runs

import (
	"encoding/json"
	"strings"
)

// StatusForEvent maps terminal App Server notifications to persisted run state.
// App Server reports all terminal turn outcomes as turn/completed; the outcome
// itself is nested in params.turn.status.
func StatusForEvent(eventType, payload string) (string, bool) {
	if strings.TrimSpace(eventType) != "turn/completed" {
		return "", false
	}
	var notification struct {
		Turn struct {
			Status string `json:"status"`
		} `json:"turn"`
	}
	if err := json.Unmarshal([]byte(payload), &notification); err != nil {
		return "", false
	}
	switch notification.Turn.Status {
	case "completed":
		return StatusCompleted, true
	case "failed":
		return StatusFailed, true
	case "interrupted":
		return StatusInterrupted, true
	default:
		return "", false
	}
}

// ErrorMessageForEvent returns the App Server's error message for failed turns.
func ErrorMessageForEvent(eventType, payload string) string {
	if strings.TrimSpace(eventType) != "turn/completed" {
		return ""
	}
	var notification struct {
		Turn struct {
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"turn"`
	}
	if err := json.Unmarshal([]byte(payload), &notification); err != nil || notification.Turn.Error == nil {
		return ""
	}
	return notification.Turn.Error.Message
}

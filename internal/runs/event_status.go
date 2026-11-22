package runs

import (
	"encoding/json"
	"strings"
)

// StatusForEvent maps terminal App Server notifications to persisted run state.
// App Server reports all terminal turn outcomes as turn/completed; the outcome
// itself is nested in params.turn.status.
func StatusForEvent(eventType, payload string) (string, bool) {
	switch strings.TrimSpace(eventType) {
	case "turn/failed":
		return StatusFailed, true
	case "turn/interrupted", "turn/aborted":
		return StatusInterrupted, true
	case "turn/completed":
	default:
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
	eventType = strings.TrimSpace(eventType)
	if eventType != "turn/completed" && eventType != "turn/failed" && eventType != "turn/error" {
		return ""
	}
	var notification struct {
		Turn struct {
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"turn"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(payload), &notification); err != nil {
		return ""
	}
	if notification.Turn.Error != nil {
		return notification.Turn.Error.Message
	}
	if notification.Error != nil {
		return notification.Error.Message
	}
	return ""
}

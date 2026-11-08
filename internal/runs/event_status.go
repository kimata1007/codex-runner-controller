package runs

import "strings"

// StatusForEvent maps App Server lifecycle notifications to persisted run state.
func StatusForEvent(eventType string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "turn/completed", "turn/complete", "turn/finished":
		return StatusCompleted, true
	case "turn/failed", "turn/error":
		return StatusFailed, true
	case "turn/interrupted", "turn/aborted":
		return StatusInterrupted, true
	case "approval/requested", "turn/approval_required", "item/approval_requested":
		return StatusWaitingApproval, true
	default:
		return "", false
	}
}

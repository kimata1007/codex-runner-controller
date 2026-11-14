package runs

import (
	"encoding/json"
	"strings"
)

// AssistantTextFromEvents reconstructs the user-visible assistant response
// from App Server delta notifications. Event payloads are intentionally kept
// opaque in the event log; this projection only understands the stable delta
// shapes and ignores tool or approval payloads.
func AssistantTextFromEvents(events []Event) string {
	var builder strings.Builder
	for _, event := range events {
		if delta := assistantDelta(event.EventType, event.Payload); delta != "" {
			builder.WriteString(delta)
		}
	}
	return builder.String()
}

func assistantDelta(eventType, payload string) string {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "agentmessage/delta", "item/agentmessage/delta", "item/agent_message/delta":
	default:
		return ""
	}
	var value any
	if json.Unmarshal([]byte(payload), &value) != nil {
		return ""
	}
	return findText(value, "delta")
}

func findText(value any, preferredKey string) string {
	switch typed := value.(type) {
	case map[string]any:
		if text, ok := typed[preferredKey].(string); ok {
			return text
		}
		for _, key := range []string{"text", "content"} {
			if text, ok := typed[key].(string); ok {
				return text
			}
		}
		for _, nested := range typed {
			if text := findText(nested, preferredKey); text != "" {
				return text
			}
		}
	case []any:
		for _, nested := range typed {
			if text := findText(nested, preferredKey); text != "" {
				return text
			}
		}
	}
	return ""
}

package runs

import "testing"

func TestAssistantTextFromEvents(t *testing.T) {
	text := AssistantTextFromEvents([]Event{
		{EventType: "item/agentMessage/delta", Payload: `{"delta":"hello "}`},
		{EventType: "item/agentMessage/delta", Payload: `{"delta":"world"}`},
		{EventType: "approval/requested", Payload: `{"text":"ignore"}`},
	})
	if text != "hello world" {
		t.Fatalf("text = %q", text)
	}
}

package session

import "context"

// Activation is the small amount of conversation state needed to create or
// resume a CodexSession resource.
type Activation struct {
	ConversationID string
	Mode           string
}

// Activator makes the session pod reachable before a run is started.
type Activator interface {
	Activate(context.Context, Activation) error
}

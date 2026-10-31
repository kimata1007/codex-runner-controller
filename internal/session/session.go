package session

import (
	"context"
	"time"
)

// Activation identifies the conversation whose runner-agent must be reachable.
type Activation struct {
	ConversationID string
	RequestedAt    time.Time
	Repository     *Repository
}

type Repository struct {
	URL      string
	Revision string
}

// Activator makes a conversation's runner-agent reachable before a run starts.
type Activator interface {
	Activate(context.Context, Activation) error
}

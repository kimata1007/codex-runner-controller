package session

import (
	"context"
	"time"
)

// DefaultRetentionPeriod is distinct from the short Pod idle timeout.
const DefaultRetentionPeriod = 7 * 24 * time.Hour

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

// ActivityRecorder updates the last activity time for a conversation session.
type ActivityRecorder interface {
	Touch(context.Context, string, time.Time) error
}

// PhaseRecorder projects the live run state onto CodexSession.status for
// operators and the controller's lifecycle decisions.
type PhaseRecorder interface {
	SetPhase(context.Context, string, string) error
}

package session

import (
	"context"
	"time"
)

// Activation is the small amount of conversation state needed to create or
// resume a CodexSession resource.
type Activation struct {
	ConversationID string
	Mode           string
	Repository     *Repository
}

type Repository struct {
	URL      string
	Revision string
}

// Activator makes the session pod reachable before a run is started.
type Activator interface {
	Activate(context.Context, Activation) error
}

// ActivityRecorder updates the last user activity without owning the
// lifecycle decision. The controller uses that timestamp to suspend a pod.
type ActivityRecorder interface {
	Touch(context.Context, string, time.Time) error
}

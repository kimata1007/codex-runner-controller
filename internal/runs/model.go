package runs

import "time"

const (
	StatusQueued          = "queued"
	StatusRunning         = "running"
	StatusWaitingApproval = "waiting_approval"
	StatusCompleted       = "completed"
	StatusFailed          = "failed"
	StatusInterrupted     = "interrupted"
)

type Run struct {
	ID             string
	ConversationID string
	MessageID      string
	ThreadID       string
	Status         string
	StartedAt      *time.Time
	CompletedAt    *time.Time
	ErrorCode      string
	ErrorMessage   string
	CreatedAt      time.Time
}

type Event struct {
	ID        string
	RunID     string
	Sequence  int64
	EventType string
	Payload   string
	CreatedAt time.Time
}

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
	ID             string     `json:"id"`
	ConversationID string     `json:"conversation_id"`
	MessageID      string     `json:"message_id"`
	ThreadID       string     `json:"thread_id"`
	Status         string     `json:"status"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	ErrorCode      string     `json:"error_code,omitempty"`
	ErrorMessage   string     `json:"error_message,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// ActiveRun identifies a non-terminal run that a re-opened PWA can reconnect to.
type ActiveRun struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type Event struct {
	ID        string
	RunID     string
	Sequence  int64
	EventType string
	Payload   string
	CreatedAt time.Time
}

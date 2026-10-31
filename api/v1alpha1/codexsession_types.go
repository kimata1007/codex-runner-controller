package v1alpha1

import "fmt"

const (
	DefaultIdleTimeoutSeconds int32 = 1800

	DesiredStateActive    = "Active"
	DesiredStateSuspended = "Suspended"

	PhaseSuspended          = "Suspended"
	PhaseStarting           = "Starting"
	PhaseReady              = "Ready"
	PhaseBusy               = "Busy"
	PhaseWaitingForApproval = "WaitingForApproval"
	PhaseFailed             = "Failed"
	PhaseTerminating        = "Terminating"
)

type CodexSessionSpec struct {
	ConversationID     string           `json:"conversationId" yaml:"conversationId"`
	DesiredState       string           `json:"desiredState" yaml:"desiredState"`
	IdleTimeoutSeconds int32            `json:"idleTimeoutSeconds,omitempty" yaml:"idleTimeoutSeconds,omitempty"`
	SessionData        SessionData      `json:"sessionData" yaml:"sessionData"`
	Execution          Execution        `json:"execution" yaml:"execution"`
	Repository         *Repository      `json:"repository,omitempty" yaml:"repository,omitempty"`
	CredentialsRef     *SecretReference `json:"credentialsRef,omitempty" yaml:"credentialsRef,omitempty"`
}

type SessionData struct {
	StorageClassName string `json:"storageClassName,omitempty" yaml:"storageClassName,omitempty"`
	Size             string `json:"size" yaml:"size"`
	Retain           bool   `json:"retain" yaml:"retain"`
}

type Execution struct {
	Model          string `json:"model,omitempty" yaml:"model,omitempty"`
	ApprovalPolicy string `json:"approvalPolicy" yaml:"approvalPolicy"`
}

type Repository struct {
	URL      string `json:"url" yaml:"url"`
	Revision string `json:"revision" yaml:"revision"`
}

type SecretReference struct {
	Name string `json:"name" yaml:"name"`
}

type CodexSessionStatus struct {
	Phase              string `json:"phase,omitempty" yaml:"phase,omitempty"`
	ThreadID           string `json:"threadId,omitempty" yaml:"threadId,omitempty"`
	PodName            string `json:"podName,omitempty" yaml:"podName,omitempty"`
	WorkspaceClaim     string `json:"workspaceClaim,omitempty" yaml:"workspaceClaim,omitempty"`
	LastActivityAt     string `json:"lastActivityAt,omitempty" yaml:"lastActivityAt,omitempty"`
	ObservedGeneration int64  `json:"observedGeneration,omitempty" yaml:"observedGeneration,omitempty"`
}

func (s CodexSessionSpec) Validate() error {
	if s.ConversationID == "" {
		return fmt.Errorf("conversationId is required")
	}
	if s.DesiredState != DesiredStateActive && s.DesiredState != DesiredStateSuspended {
		return fmt.Errorf("desiredState must be %q or %q", DesiredStateActive, DesiredStateSuspended)
	}
	if s.IdleTimeoutSeconds < 0 {
		return fmt.Errorf("idleTimeoutSeconds must not be negative")
	}
	if s.SessionData.Size == "" {
		return fmt.Errorf("sessionData.size is required")
	}
	if s.Execution.ApprovalPolicy == "" {
		return fmt.Errorf("execution.approvalPolicy is required")
	}
	if s.Repository != nil && (s.Repository.URL == "" || s.Repository.Revision == "") {
		return fmt.Errorf("repository.url and repository.revision are required together")
	}
	return nil
}

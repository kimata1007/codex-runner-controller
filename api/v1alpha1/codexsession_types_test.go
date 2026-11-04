package v1alpha1

import "testing"

func validSpec() CodexSessionSpec {
	return CodexSessionSpec{
		ConversationID: "conversation-1",
		DesiredState:   DesiredStateActive,
		SessionData:    SessionData{Size: "10Gi", Retain: true},
		Execution:      Execution{ApprovalPolicy: "user"},
	}
}

func TestCodexSessionSpecValidate(t *testing.T) {
	tests := []struct {
		name string
		edit func(*CodexSessionSpec)
		want bool
	}{
		{name: "valid without repository", want: true},
		{name: "valid with repository", edit: func(spec *CodexSessionSpec) {
			spec.Repository = &Repository{URL: "https://github.com/example/repo.git", Revision: "main"}
		}, want: true},
		{name: "missing conversation", edit: func(spec *CodexSessionSpec) {
			spec.ConversationID = ""
		}, want: false},
		{name: "invalid desired state", edit: func(spec *CodexSessionSpec) {
			spec.DesiredState = "Paused"
		}, want: false},
		{name: "missing session size", edit: func(spec *CodexSessionSpec) {
			spec.SessionData.Size = ""
		}, want: false},
		{name: "missing approval policy", edit: func(spec *CodexSessionSpec) {
			spec.Execution.ApprovalPolicy = ""
		}, want: false},
		{name: "repository requires revision", edit: func(spec *CodexSessionSpec) {
			spec.Repository = &Repository{URL: "https://github.com/example/repo.git"}
		}, want: false},
		{name: "negative idle timeout", edit: func(spec *CodexSessionSpec) {
			spec.IdleTimeoutSeconds = -1
		}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := validSpec()
			if tt.edit != nil {
				tt.edit(&spec)
			}
			if got := spec.Validate() == nil; got != tt.want {
				t.Fatalf("Validate() success = %v, want %v", got, tt.want)
			}
		})
	}
}

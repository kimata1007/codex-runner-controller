package kubernetes

import (
	"context"
	"testing"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/session"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSessionActivatorCreatesSessionAndWaitsUntilReady(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	base := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha1.CodexSession{}).Build()
	kubeClient := &readyOnWriteClient{Client: base}
	activator := &SessionActivator{Client: kubeClient, Namespace: "sessions", ReadyPollInterval: time.Millisecond}

	activityAt := time.Date(2026, 10, 31, 10, 20, 30, 0, time.FixedZone("JST", 9*60*60))
	repository := &session.Repository{URL: "https://example.test/repo.git", Revision: "main"}
	if err := activator.Activate(context.Background(), session.Activation{ConversationID: "conversation-1", RequestedAt: activityAt, Repository: repository}); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	created := &v1alpha1.CodexSession{}
	if err := base.Get(context.Background(), client.ObjectKey{Namespace: "sessions", Name: "conversation-1"}, created); err != nil {
		t.Fatalf("get created session: %v", err)
	}
	if created.Spec.ConversationID != "conversation-1" || created.Spec.DesiredState != v1alpha1.DesiredStateActive {
		t.Fatalf("created session spec = %+v", created.Spec)
	}
	if created.Spec.SessionData.Size != DefaultSessionDataSize || created.Spec.Execution.ApprovalPolicy != DefaultApprovalPolicy {
		t.Fatalf("created session defaults = %+v", created.Spec)
	}
	if created.Spec.Repository == nil || created.Spec.Repository.URL != repository.URL || created.Spec.Repository.Revision != repository.Revision {
		t.Fatalf("created session repository = %+v", created.Spec.Repository)
	}
	if created.Status.LastActivityAt != "2026-10-31T01:20:30Z" {
		t.Fatalf("last activity = %q", created.Status.LastActivityAt)
	}
}

func TestSessionActivatorUsesCredentialsSecretReference(t *testing.T) {
	activator := &SessionActivator{CredentialsRefName: "github-credentials"}
	spec := activator.newSpec("conversation-1", nil)
	if spec.CredentialsRef == nil || spec.CredentialsRef.Name != "github-credentials" {
		t.Fatalf("credentials ref = %+v", spec.CredentialsRef)
	}
}

func TestSessionActivatorResumesSuspendedSession(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	existing := &v1alpha1.CodexSession{
		ObjectMeta: metav1.ObjectMeta{Name: "conversation-1", Namespace: "sessions"},
		Spec: v1alpha1.CodexSessionSpec{
			ConversationID: "conversation-1",
			DesiredState:   v1alpha1.DesiredStateSuspended,
		},
	}
	base := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha1.CodexSession{}).WithObjects(existing).Build()
	kubeClient := &readyOnWriteClient{Client: base}
	activator := &SessionActivator{Client: kubeClient, Namespace: "sessions", ReadyPollInterval: time.Millisecond}

	if err := activator.Activate(context.Background(), session.Activation{ConversationID: "conversation-1"}); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	updated := &v1alpha1.CodexSession{}
	if err := base.Get(context.Background(), client.ObjectKeyFromObject(existing), updated); err != nil {
		t.Fatalf("get resumed session: %v", err)
	}
	if updated.Spec.DesiredState != v1alpha1.DesiredStateActive {
		t.Fatalf("desired state = %q, want Active", updated.Spec.DesiredState)
	}
}

func TestSessionEndpointResolverUsesSanitizedSessionName(t *testing.T) {
	endpoint, err := (SessionEndpointResolver{Namespace: "sessions"}).Resolve(context.Background(), "Conversation/42")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if endpoint != "http://conversation-42.sessions.svc:8081" {
		t.Fatalf("endpoint = %q", endpoint)
	}
}

type readyOnWriteClient struct{ client.Client }

func (c *readyOnWriteClient) Get(ctx context.Context, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
	if err := c.Client.Get(ctx, key, object, options...); err != nil {
		return err
	}
	if session, ok := object.(*v1alpha1.CodexSession); ok {
		session.Status.Phase = v1alpha1.PhaseReady
		session.Status.ObservedGeneration = session.Generation
	}
	return nil
}

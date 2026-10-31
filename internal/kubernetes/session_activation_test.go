package kubernetes

import (
	"context"
	"testing"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/session"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func activationScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func activationDefaults() v1alpha1.CodexSessionSpec {
	return v1alpha1.CodexSessionSpec{DesiredState: v1alpha1.DesiredStateActive, SessionData: v1alpha1.SessionData{Size: "1Gi"}, Execution: v1alpha1.Execution{ApprovalPolicy: "user"}}
}

func TestSessionActivatorCreatesActiveSession(t *testing.T) {
	kubeClient := fake.NewClientBuilder().WithScheme(activationScheme(t)).Build()
	activator := SessionActivator{Client: kubeClient, Namespace: "codex-system", Defaults: activationDefaults()}
	if err := activator.Activate(context.Background(), session.Activation{ConversationID: "conversation-1"}); err != nil {
		t.Fatalf("activate: %v", err)
	}
	resource := &v1alpha1.CodexSession{}
	if err := kubeClient.Get(context.Background(), client.ObjectKey{Namespace: "codex-system", Name: "conversation-1"}, resource); err != nil {
		t.Fatalf("get session: %v", err)
	}
	if resource.Spec.DesiredState != v1alpha1.DesiredStateActive || resource.Spec.ConversationID != "conversation-1" {
		t.Fatalf("spec = %+v", resource.Spec)
	}
}

func TestSessionActivatorResumesExistingSession(t *testing.T) {
	existing := &v1alpha1.CodexSession{ObjectMeta: metav1.ObjectMeta{Name: "conversation-1", Namespace: "codex-system"}, Spec: activationDefaults()}
	existing.Spec.DesiredState = v1alpha1.DesiredStateSuspended
	kubeClient := fake.NewClientBuilder().WithScheme(activationScheme(t)).WithObjects(existing).Build()
	activator := SessionActivator{Client: kubeClient, Namespace: "codex-system", Defaults: activationDefaults()}
	if err := activator.Activate(context.Background(), session.Activation{ConversationID: "conversation-1"}); err != nil {
		t.Fatalf("activate: %v", err)
	}
	updated := &v1alpha1.CodexSession{}
	if err := kubeClient.Get(context.Background(), client.ObjectKey{Namespace: "codex-system", Name: "conversation-1"}, updated); err != nil {
		t.Fatalf("get session: %v", err)
	}
	if updated.Spec.DesiredState != v1alpha1.DesiredStateActive {
		t.Fatalf("desired state = %q", updated.Spec.DesiredState)
	}
}

func TestSessionActivatorRecordsActivity(t *testing.T) {
	existing := &v1alpha1.CodexSession{ObjectMeta: metav1.ObjectMeta{Name: "conversation-1", Namespace: "codex-system"}, Spec: activationDefaults()}
	kubeClient := fake.NewClientBuilder().WithScheme(activationScheme(t)).WithStatusSubresource(existing).WithObjects(existing).Build()
	activator := SessionActivator{Client: kubeClient, Namespace: "codex-system", Defaults: activationDefaults()}
	activityAt := time.Date(2026, 10, 31, 10, 20, 30, 0, time.FixedZone("JST", 9*60*60))
	if err := activator.Touch(context.Background(), "conversation-1", activityAt); err != nil {
		t.Fatalf("touch: %v", err)
	}
	updated := &v1alpha1.CodexSession{}
	if err := kubeClient.Get(context.Background(), client.ObjectKey{Namespace: "codex-system", Name: "conversation-1"}, updated); err != nil {
		t.Fatalf("get session: %v", err)
	}
	if updated.Status.LastActivityAt != "2026-10-31T01:20:30Z" {
		t.Fatalf("last activity = %q", updated.Status.LastActivityAt)
	}
}

func TestSessionEndpointResolverUsesServiceDNS(t *testing.T) {
	endpoint, err := (SessionEndpointResolver{Namespace: "codex-system"}).Resolve(context.Background(), "conversation-1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if endpoint != "http://conversation-1.codex-system.svc.cluster.local:8081" {
		t.Fatalf("endpoint = %q", endpoint)
	}
}

func TestSessionNameSanitizesConversationID(t *testing.T) {
	name, err := SessionName(" Conversation/ABC ")
	if err != nil || name != "conversation-abc" {
		t.Fatalf("name = %q, err = %v", name, err)
	}
}

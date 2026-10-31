package kubernetes

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/session"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const DefaultSessionNamespace = "codex-system"

// SessionActivator creates the stable CodexSession object used by the
// controller. It only changes desired state; the controller owns Pod/PVC/
// Service creation and readiness.
type SessionActivator struct {
	Client    client.Client
	Namespace string
	Defaults  v1alpha1.CodexSessionSpec
}

func (a SessionActivator) Activate(ctx context.Context, activation session.Activation) error {
	if a.Client == nil {
		return fmt.Errorf("session activator: client is required")
	}
	name, err := SessionName(activation.ConversationID)
	if err != nil {
		return err
	}
	namespace := a.Namespace
	if namespace == "" {
		namespace = DefaultSessionNamespace
	}
	key := client.ObjectKey{Namespace: namespace, Name: name}
	existing := &v1alpha1.CodexSession{}
	err = a.Client.Get(ctx, key, existing)
	if apierrors.IsNotFound(err) {
		spec := a.Defaults
		spec.ConversationID = activation.ConversationID
		spec.DesiredState = v1alpha1.DesiredStateActive
		if err := spec.Validate(); err != nil {
			return fmt.Errorf("session activator: validate defaults: %w", err)
		}
		resource := &v1alpha1.CodexSession{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}, Spec: spec}
		if err := a.Client.Create(ctx, resource); err != nil {
			return fmt.Errorf("session activator: create session: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("session activator: get session: %w", err)
	}
	if existing.Spec.DesiredState == v1alpha1.DesiredStateActive {
		return nil
	}
	existing.Spec.DesiredState = v1alpha1.DesiredStateActive
	if err := a.Client.Update(ctx, existing); err != nil {
		return fmt.Errorf("session activator: resume session: %w", err)
	}
	return nil
}

func (a SessionActivator) Touch(ctx context.Context, conversationID string, at time.Time) error {
	if a.Client == nil {
		return fmt.Errorf("session activity: client is required")
	}
	name, err := SessionName(conversationID)
	if err != nil {
		return err
	}
	namespace := a.Namespace
	if namespace == "" {
		namespace = DefaultSessionNamespace
	}
	resource := &v1alpha1.CodexSession{}
	if err := a.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, resource); err != nil {
		return fmt.Errorf("session activity: get session: %w", err)
	}
	resource.Status.LastActivityAt = at.UTC().Format(time.RFC3339Nano)
	if err := a.Client.Status().Update(ctx, resource); err != nil {
		return fmt.Errorf("session activity: update status: %w", err)
	}
	return nil
}

// SessionName applies the Kubernetes DNS label constraints to the stable
// conversation-to-Service identity.
func SessionName(conversationID string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(conversationID))
	if name == "" {
		return "", fmt.Errorf("session name: conversation id is required")
	}
	var builder strings.Builder
	lastDash := false
	for _, char := range name {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	name = strings.Trim(builder.String(), "-")
	if len(name) > 63 {
		name = strings.TrimRight(name[:63], "-")
	}
	if name == "" {
		return "", fmt.Errorf("session name: conversation id has no DNS-safe characters")
	}
	return name, nil
}

// SessionEndpointResolver resolves the Service DNS name created by the
// CodexSession controller.
type SessionEndpointResolver struct {
	Namespace string
	Port      int
}

func (r SessionEndpointResolver) Resolve(_ context.Context, conversationID string) (string, error) {
	name, err := SessionName(conversationID)
	if err != nil {
		return "", err
	}
	namespace := r.Namespace
	if namespace == "" {
		namespace = DefaultSessionNamespace
	}
	port := r.Port
	if port == 0 {
		port = RunnerAgentPort
	}
	return (&url.URL{Scheme: "http", Host: fmt.Sprintf("%s.%s.svc.cluster.local:%d", name, namespace, port)}).String(), nil
}

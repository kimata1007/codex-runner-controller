package kubernetes

import (
	"context"
	"fmt"
	"strings"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/session"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultSessionNamespace    = "codex-system"
	DefaultSessionDataSize     = "10Gi"
	DefaultApprovalPolicy      = "never"
	DefaultSessionReadyTimeout = 60 * time.Second
)

// SessionActivator creates or resumes a CodexSession and waits for its controller to
// make the runner-agent Service ready before a message is dispatched.
type SessionActivator struct {
	Client             client.Client
	Namespace          string
	SessionDataSize    string
	StorageClassName   string
	CredentialsRefName string
	IdleTimeoutSeconds int32
	Model              string
	ApprovalPolicy     string
	ReadyPollInterval  time.Duration
	ReadyTimeout       time.Duration
}

func (a *SessionActivator) Activate(ctx context.Context, activation session.Activation) error {
	if a == nil || a.Client == nil {
		return fmt.Errorf("Kubernetes client is required to activate a session")
	}
	conversationID := activation.ConversationID
	if conversationID == "" {
		return fmt.Errorf("conversation id is required")
	}
	name, err := SessionName(conversationID)
	if err != nil {
		return err
	}
	namespace := a.Namespace
	if namespace == "" {
		namespace = DefaultSessionNamespace
	}

	key := types.NamespacedName{Namespace: namespace, Name: name}
	resource := &v1alpha1.CodexSession{}
	err = a.Client.Get(ctx, key, resource)
	if apierrors.IsNotFound(err) {
		resource = &v1alpha1.CodexSession{
			TypeMeta:   metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "CodexSession"},
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec:       a.newSpec(conversationID, activation.Repository),
		}
		if err := a.Client.Create(ctx, resource); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("create CodexSession: %w", err)
			}
			if err := a.Client.Get(ctx, key, resource); err != nil {
				return fmt.Errorf("read concurrently created CodexSession: %w", err)
			}
		}
	} else if err != nil {
		return fmt.Errorf("get CodexSession: %w", err)
	}
	repositoryChanged := activation.Repository != nil && (resource.Spec.Repository == nil ||
		resource.Spec.Repository.URL != activation.Repository.URL || resource.Spec.Repository.Revision != activation.Repository.Revision)
	if resource.Spec.DesiredState != v1alpha1.DesiredStateActive || repositoryChanged {
		resource.Spec.DesiredState = v1alpha1.DesiredStateActive
		if activation.Repository != nil {
			resource.Spec.Repository = &v1alpha1.Repository{URL: activation.Repository.URL, Revision: activation.Repository.Revision}
		}
		if err := a.Client.Update(ctx, resource); err != nil {
			return fmt.Errorf("update CodexSession: %w", err)
		}
	}
	if err := a.recordActivity(ctx, key, activation.RequestedAt); err != nil {
		return fmt.Errorf("record session activity: %w", err)
	}

	readyTimeout := a.ReadyTimeout
	if readyTimeout <= 0 {
		readyTimeout = DefaultSessionReadyTimeout
	}
	readyCtx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()

	interval := a.ReadyPollInterval
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := a.Client.Get(readyCtx, key, resource); err != nil {
			return fmt.Errorf("read CodexSession status: %w", err)
		}
		if resource.Spec.DesiredState != v1alpha1.DesiredStateActive {
			return fmt.Errorf("CodexSession %q is not active", name)
		}
		if resource.Status.Phase == v1alpha1.PhaseBusy || resource.Status.Phase == v1alpha1.PhaseWaitingForApproval {
			return session.ErrSessionHasActiveRun
		}
		if resource.Status.Phase == v1alpha1.PhaseFailed {
			return fmt.Errorf("CodexSession %q failed to start", name)
		}
		if resource.Status.Phase == v1alpha1.PhaseReady && resource.Status.ObservedGeneration >= resource.Generation {
			return nil
		}
		select {
		case <-readyCtx.Done():
			return fmt.Errorf("wait for CodexSession %q readiness: %w", name, readyCtx.Err())
		case <-ticker.C:
		}
	}
}

func (a *SessionActivator) newSpec(conversationID string, repository *session.Repository) v1alpha1.CodexSessionSpec {
	size := a.SessionDataSize
	if size == "" {
		size = DefaultSessionDataSize
	}
	idleTimeout := a.IdleTimeoutSeconds
	if idleTimeout == 0 {
		idleTimeout = v1alpha1.DefaultIdleTimeoutSeconds
	}
	approvalPolicy := a.ApprovalPolicy
	if approvalPolicy == "" {
		approvalPolicy = DefaultApprovalPolicy
	}
	spec := v1alpha1.CodexSessionSpec{
		ConversationID:     conversationID,
		DesiredState:       v1alpha1.DesiredStateActive,
		IdleTimeoutSeconds: idleTimeout,
		SessionData: v1alpha1.SessionData{
			StorageClassName: a.StorageClassName,
			Size:             size,
			// The PVC outlives idle Pod deletion because the CodexSession
			// remains. Delete it with the CodexSession unless an operator
			// explicitly opts into retaining it after session deletion.
			Retain: false,
		},
		Execution: v1alpha1.Execution{Model: a.Model, ApprovalPolicy: approvalPolicy},
	}
	if a.CredentialsRefName != "" {
		spec.CredentialsRef = &v1alpha1.SecretReference{Name: a.CredentialsRefName}
	}
	if repository != nil {
		spec.Repository = &v1alpha1.Repository{URL: repository.URL, Revision: repository.Revision}
	}
	return spec
}

func (a *SessionActivator) recordActivity(ctx context.Context, key types.NamespacedName, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		resource := &v1alpha1.CodexSession{}
		if err := a.Client.Get(ctx, key, resource); err != nil {
			return err
		}
		resource.Status.LastActivityAt = at.Format(time.RFC3339Nano)
		return a.Client.Status().Update(ctx, resource)
	})
}

// Touch records heartbeat activity without changing the session lifecycle.
func (a *SessionActivator) Touch(ctx context.Context, conversationID string, at time.Time) error {
	if a == nil || a.Client == nil {
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
	key := types.NamespacedName{Namespace: namespace, Name: name}
	if err := a.recordActivity(ctx, key, at); err != nil {
		return fmt.Errorf("session activity: update status: %w", err)
	}
	return nil
}

func (a SessionActivator) SetPhase(ctx context.Context, conversationID, phase string) error {
	if a.Client == nil {
		return fmt.Errorf("session phase: client is required")
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
		return fmt.Errorf("session phase: get session: %w", err)
	}
	if resource.Status.Phase == phase {
		return nil
	}
	resource.Status.Phase = phase
	if err := a.Client.Status().Update(ctx, resource); err != nil {
		return fmt.Errorf("session phase: update status: %w", err)
	}
	return nil
}

// SessionName maps a conversation ID to the Kubernetes DNS label used by its
// CodexSession and Service.
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
	return fmt.Sprintf("http://%s.%s.svc:%d", name, namespace, port), nil
}

// AddToScheme registers the built-in types used by an API-side Kubernetes client.
func AddToScheme(scheme *runtime.Scheme) error {
	if err := corev1.AddToScheme(scheme); err != nil {
		return err
	}
	return v1alpha1.AddToScheme(scheme)
}

var _ session.Activator = (*SessionActivator)(nil)

// DeleteSession removes the session resource. Kubernetes owner references clean up
// its Pod, Service and (unless retained) PVC asynchronously.
func (a *SessionActivator) DeleteSession(ctx context.Context, conversationID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a == nil || a.Client == nil {
		return fmt.Errorf("Kubernetes client is required to delete a session")
	}
	name, err := SessionName(conversationID)
	if err != nil {
		return err
	}
	namespace := a.Namespace
	if namespace == "" {
		namespace = DefaultSessionNamespace
	}
	value := &v1alpha1.CodexSession{}
	key := types.NamespacedName{Namespace: namespace, Name: name}
	if err := a.Client.Get(ctx, key, value); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("read CodexSession for deletion: %w", err)
	}
	if value.Spec.ConversationID != conversationID {
		return fmt.Errorf("CodexSession conversation identity mismatch")
	}
	if err := a.Client.Delete(ctx, value); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete CodexSession: %w", err)
	}
	return nil
}

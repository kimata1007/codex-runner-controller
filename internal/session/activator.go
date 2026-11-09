package session

import (
	"context"
	"fmt"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultNamespace          = "codex-system"
	DefaultSessionDataSize    = "10Gi"
	DefaultIdleTimeoutSeconds = int32(1800)
	DefaultApprovalPolicy     = "user"
)

// Activator creates or resumes a CodexSession and waits for its controller to
// make the runner-agent Service ready before a message is dispatched.
type Activator struct {
	Client             client.Client
	Namespace          string
	SessionDataSize    string
	StorageClassName   string
	IdleTimeoutSeconds int32
	Model              string
	ApprovalPolicy     string
	ReadyPollInterval  time.Duration
}

func (a *Activator) Activate(ctx context.Context, conversationID string) error {
	if a == nil || a.Client == nil {
		return fmt.Errorf("Kubernetes client is required to activate a session")
	}
	if conversationID == "" {
		return fmt.Errorf("conversation id is required")
	}
	namespace := a.Namespace
	if namespace == "" {
		namespace = DefaultNamespace
	}

	key := types.NamespacedName{Namespace: namespace, Name: conversationID}
	session := &v1alpha1.CodexSession{}
	err := a.Client.Get(ctx, key, session)
	if apierrors.IsNotFound(err) {
		session = &v1alpha1.CodexSession{
			TypeMeta:   metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "CodexSession"},
			ObjectMeta: metav1.ObjectMeta{Name: conversationID, Namespace: namespace},
			Spec:       a.newSpec(conversationID),
		}
		if err := a.Client.Create(ctx, session); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("create CodexSession: %w", err)
			}
			if err := a.Client.Get(ctx, key, session); err != nil {
				return fmt.Errorf("read concurrently created CodexSession: %w", err)
			}
		}
	} else if err != nil {
		return fmt.Errorf("get CodexSession: %w", err)
	}
	if session.Spec.DesiredState != v1alpha1.DesiredStateActive {
		session.Spec.DesiredState = v1alpha1.DesiredStateActive
		if err := a.Client.Update(ctx, session); err != nil {
			return fmt.Errorf("resume CodexSession: %w", err)
		}
	}

	interval := a.ReadyPollInterval
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := a.Client.Get(ctx, key, session); err != nil {
			return fmt.Errorf("read CodexSession status: %w", err)
		}
		if session.Spec.DesiredState != v1alpha1.DesiredStateActive {
			return fmt.Errorf("CodexSession %q is not active", conversationID)
		}
		if session.Status.Phase == v1alpha1.PhaseFailed {
			return fmt.Errorf("CodexSession %q failed to start", conversationID)
		}
		if session.Status.Phase == v1alpha1.PhaseReady && session.Status.ObservedGeneration >= session.Generation {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for CodexSession %q readiness: %w", conversationID, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (a *Activator) newSpec(conversationID string) v1alpha1.CodexSessionSpec {
	size := a.SessionDataSize
	if size == "" {
		size = DefaultSessionDataSize
	}
	idleTimeout := a.IdleTimeoutSeconds
	if idleTimeout == 0 {
		idleTimeout = DefaultIdleTimeoutSeconds
	}
	approvalPolicy := a.ApprovalPolicy
	if approvalPolicy == "" {
		approvalPolicy = DefaultApprovalPolicy
	}
	return v1alpha1.CodexSessionSpec{
		ConversationID:     conversationID,
		DesiredState:       v1alpha1.DesiredStateActive,
		IdleTimeoutSeconds: idleTimeout,
		SessionData: v1alpha1.SessionData{
			StorageClassName: a.StorageClassName,
			Size:             size,
			Retain:           true,
		},
		Execution: v1alpha1.Execution{Model: a.Model, ApprovalPolicy: approvalPolicy},
	}
}

// AddToScheme registers the built-in types used by an API-side Kubernetes client.
func AddToScheme(scheme *runtime.Scheme) error {
	if err := corev1.AddToScheme(scheme); err != nil {
		return err
	}
	return v1alpha1.AddToScheme(scheme)
}

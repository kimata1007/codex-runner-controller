package controller

import (
	"context"
	"fmt"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/kubernetes"
	sessionpkg "github.com/kimata1007/codex-runner-controller/internal/session"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type CodexSessionController struct {
	client.Client
	Builder         kubernetes.ResourceBuilder
	RetentionPeriod time.Duration
	Now             func() time.Time
}

func (r *CodexSessionController) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	session := &v1alpha1.CodexSession{}
	if err := r.Client.Get(ctx, request.NamespacedName, session); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	retentionPeriod := r.RetentionPeriod
	if retentionPeriod <= 0 {
		retentionPeriod = sessionpkg.DefaultRetentionPeriod
	}
	now := time.Now().UTC()
	if r.Now != nil {
		now = r.Now().UTC()
	}
	deadline, hasDeadline, err := sessionRetentionDeadline(session, retentionPeriod)
	if err != nil {
		return reconcile.Result{}, err
	}
	if hasDeadline && !now.Before(deadline) {
		pvc := &corev1.PersistentVolumeClaim{}
		pvc.Namespace = session.Namespace
		pvc.Name = session.Name
		if err := r.Client.Delete(ctx, pvc); err != nil && client.IgnoreNotFound(err) != nil {
			return reconcile.Result{}, fmt.Errorf("delete expired session PVC: %w", err)
		}
		if err := r.Client.Delete(ctx, session); err != nil && client.IgnoreNotFound(err) != nil {
			return reconcile.Result{}, fmt.Errorf("delete expired CodexSession: %w", err)
		}
		return reconcile.Result{}, nil
	}

	logic := CodexSessionReconciler{Client: KubernetesResourceClient{
		Client: r.Client, Owner: session, RetainPVC: session.Spec.SessionData.Retain,
	}, Builder: r.Builder}
	result, err := logic.ReconcileStatus(ctx, session.Namespace, session.Name, session.Spec, session.Status)
	if err != nil {
		return reconcile.Result{}, err
	}
	previousStatus := session.Status
	session.Status.Phase = result.Phase
	session.Status.PodName = result.PodName
	session.Status.WorkspaceClaim = result.WorkspaceClaim
	session.Status.ObservedGeneration = session.Generation
	if session.Status != previousStatus {
		if err := r.Client.Status().Update(ctx, session); err != nil {
			return reconcile.Result{}, err
		}
	}
	if hasDeadline {
		retentionRequeue := deadline.Sub(now)
		if retentionRequeue > 0 && (result.RequeueAfter <= 0 || retentionRequeue < result.RequeueAfter) {
			result.RequeueAfter = retentionRequeue
		}
	}
	return reconcile.Result{RequeueAfter: result.RequeueAfter}, nil
}

func sessionRetentionDeadline(session *v1alpha1.CodexSession, retentionPeriod time.Duration) (time.Time, bool, error) {
	lastActivity := session.CreationTimestamp.Time
	if session.Status.LastActivityAt != "" {
		parsed, err := time.Parse(time.RFC3339Nano, session.Status.LastActivityAt)
		if err != nil {
			return time.Time{}, false, fmt.Errorf("parse CodexSession lastActivityAt: %w", err)
		}
		lastActivity = parsed
	}
	if lastActivity.IsZero() {
		return time.Time{}, false, nil
	}
	return lastActivity.Add(retentionPeriod), true, nil
}

func (r *CodexSessionController) SetupWithManager(manager manager.Manager) error {
	return builder.ControllerManagedBy(manager).
		For(&v1alpha1.CodexSession{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Complete(r)
}

var _ reconcile.Reconciler = (*CodexSessionController)(nil)

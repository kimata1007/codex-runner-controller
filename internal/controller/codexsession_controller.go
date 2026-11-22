package controller

import (
	"context"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/kubernetes"
	"github.com/kimata1007/codex-runner-controller/internal/telemetry"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type CodexSessionController struct {
	client.Client
	Builder kubernetes.ResourceBuilder
}

func (r *CodexSessionController) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	ctx, span := telemetry.Tracer().Start(ctx, "controller.reconcile")
	defer span.End()
	session := &v1alpha1.CodexSession{}
	if err := r.Client.Get(ctx, request.NamespacedName, session); err != nil {
		telemetry.RecordError(ctx, client.IgnoreNotFound(err))
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	logic := CodexSessionReconciler{Client: KubernetesResourceClient{
		Client: r.Client, Owner: session, RetainPVC: session.Spec.SessionData.Retain,
	}, Builder: r.Builder}
	result, err := logic.ReconcileStatus(ctx, session.Namespace, session.Name, session.Spec, session.Status)
	if err != nil {
		telemetry.RecordError(ctx, err)
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
	return reconcile.Result{RequeueAfter: result.RequeueAfter}, nil
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

package controller

import (
	"context"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/kubernetes"
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
	session := &v1alpha1.CodexSession{}
	if err := r.Client.Get(ctx, request.NamespacedName, session); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	logic := CodexSessionReconciler{Client: KubernetesResourceClient{Client: r.Client, Owner: session}, Builder: r.Builder}
	result, err := logic.Reconcile(ctx, session.Namespace, session.Name, session.Spec)
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
	return reconcile.Result{}, nil
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

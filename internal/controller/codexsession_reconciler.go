package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/kubernetes"
	corev1 "k8s.io/api/core/v1"
)

var ErrNotFound = errors.New("resource not found")

type ResourceClient interface {
	GetPVC(context.Context, string, string) (*corev1.PersistentVolumeClaim, error)
	CreatePVC(context.Context, *corev1.PersistentVolumeClaim) error
	GetService(context.Context, string, string) (*corev1.Service, error)
	CreateService(context.Context, *corev1.Service) error
	GetPod(context.Context, string, string) (*corev1.Pod, error)
	CreatePod(context.Context, *corev1.Pod) error
	DeletePod(context.Context, string, string) error
}

type Result struct {
	Phase          string
	PodName        string
	WorkspaceClaim string
	RequeueAfter   time.Duration
}

type CodexSessionReconciler struct {
	Client  ResourceClient
	Builder kubernetes.ResourceBuilder
}

func (r CodexSessionReconciler) Reconcile(ctx context.Context, namespace, name string, spec v1alpha1.CodexSessionSpec) (Result, error) {
	return r.ReconcileStatus(ctx, namespace, name, spec, v1alpha1.CodexSessionStatus{})
}

func (r CodexSessionReconciler) ReconcileStatus(ctx context.Context, namespace, name string, spec v1alpha1.CodexSessionSpec, status v1alpha1.CodexSessionStatus) (Result, error) {
	if r.Client == nil {
		return Result{}, fmt.Errorf("resource client is required")
	}
	if err := spec.Validate(); err != nil {
		return Result{}, err
	}
	if spec.DesiredState == v1alpha1.DesiredStateSuspended {
		if err := r.deletePodIfPresent(ctx, namespace, name); err != nil {
			return Result{}, err
		}
		return Result{Phase: v1alpha1.PhaseSuspended, WorkspaceClaim: name}, nil
	}
	idle, requeueAfter, err := idleState(spec, status, time.Now().UTC())
	if err != nil {
		return Result{}, err
	}
	if idle {
		if err := r.deletePodIfPresent(ctx, namespace, name); err != nil {
			return Result{}, err
		}
		return Result{Phase: v1alpha1.PhaseSuspended, WorkspaceClaim: name}, nil
	}

	if _, err := r.Client.GetPVC(ctx, namespace, name); errors.Is(err, ErrNotFound) {
		pvc, err := r.Builder.BuildPVC(namespace, name, spec)
		if err != nil {
			return Result{}, err
		}
		if err := r.Client.CreatePVC(ctx, pvc); err != nil {
			return Result{}, fmt.Errorf("create pvc: %w", err)
		}
	} else if err != nil {
		return Result{}, fmt.Errorf("get pvc: %w", err)
	}

	service, err := r.Builder.BuildService(namespace, name, spec)
	if err != nil {
		return Result{}, err
	}
	if _, err := r.Client.GetService(ctx, namespace, name); errors.Is(err, ErrNotFound) {
		if err := r.Client.CreateService(ctx, service); err != nil {
			return Result{}, fmt.Errorf("create service: %w", err)
		}
	} else if err != nil {
		return Result{}, fmt.Errorf("get service: %w", err)
	}

	pod, err := r.Client.GetPod(ctx, namespace, name)
	if errors.Is(err, ErrNotFound) {
		pod, err = r.Builder.BuildPod(namespace, name, spec)
		if err != nil {
			return Result{}, err
		}
		if err := r.Client.CreatePod(ctx, pod); err != nil {
			return Result{}, fmt.Errorf("create pod: %w", err)
		}
		return Result{Phase: v1alpha1.PhaseStarting, PodName: name, WorkspaceClaim: name, RequeueAfter: requeueAfter}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("get pod: %w", err)
	}
	if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
		if err := r.Client.DeletePod(ctx, namespace, name); err != nil && !errors.Is(err, ErrNotFound) {
			return Result{}, fmt.Errorf("delete terminal pod: %w", err)
		}
		return Result{Phase: v1alpha1.PhaseStarting, WorkspaceClaim: name, RequeueAfter: time.Second}, nil
	}
	phase := v1alpha1.PhaseStarting
	if podReady(pod) {
		phase = readyPhase(status.Phase)
	}
	return Result{Phase: phase, PodName: name, WorkspaceClaim: name, RequeueAfter: requeueAfter}, nil
}

func readyPhase(current string) string {
	switch current {
	case v1alpha1.PhaseBusy, v1alpha1.PhaseWaitingForApproval:
		return current
	default:
		return v1alpha1.PhaseReady
	}
}

func idleState(spec v1alpha1.CodexSessionSpec, status v1alpha1.CodexSessionStatus, now time.Time) (bool, time.Duration, error) {
	if spec.IdleTimeoutSeconds <= 0 || status.LastActivityAt == "" {
		return false, 0, nil
	}
	if status.Phase == v1alpha1.PhaseStarting || status.Phase == v1alpha1.PhaseBusy || status.Phase == v1alpha1.PhaseWaitingForApproval {
		return false, 0, nil
	}
	lastActivity, err := time.Parse(time.RFC3339Nano, status.LastActivityAt)
	if err != nil {
		return false, 0, fmt.Errorf("parse lastActivityAt: %w", err)
	}
	deadline := lastActivity.Add(time.Duration(spec.IdleTimeoutSeconds) * time.Second)
	if !now.Before(deadline) {
		return true, 0, nil
	}
	return false, deadline.Sub(now), nil
}

func (r CodexSessionReconciler) deletePodIfPresent(ctx context.Context, namespace, name string) error {
	_, err := r.Client.GetPod(ctx, namespace, name)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get pod for deletion: %w", err)
	}
	if err := r.Client.DeletePod(ctx, namespace, name); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete pod: %w", err)
	}
	return nil
}

func podReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

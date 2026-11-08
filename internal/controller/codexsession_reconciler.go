package controller

import (
	"context"
	"errors"
	"fmt"

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
}

type CodexSessionReconciler struct {
	Client  ResourceClient
	Builder kubernetes.ResourceBuilder
}

func (r CodexSessionReconciler) Reconcile(ctx context.Context, namespace, name string, spec v1alpha1.CodexSessionSpec) (Result, error) {
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
		return Result{Phase: v1alpha1.PhaseStarting, PodName: name, WorkspaceClaim: name}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("get pod: %w", err)
	}
	phase := v1alpha1.PhaseStarting
	if podReady(pod) {
		phase = v1alpha1.PhaseReady
	}
	return Result{Phase: phase, PodName: name, WorkspaceClaim: name}, nil
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

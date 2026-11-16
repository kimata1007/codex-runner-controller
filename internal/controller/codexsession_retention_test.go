package controller

import (
	"context"
	"testing"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/kubernetes"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestControllerDeletesExpiredSessionAndOwnedResources(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	resource := &v1alpha1.CodexSession{
		ObjectMeta: metav1.ObjectMeta{Name: "session-1", Namespace: "codex-system", CreationTimestamp: metav1.NewTime(now.Add(-8 * 24 * time.Hour))},
		Status:     v1alpha1.CodexSessionStatus{Phase: v1alpha1.PhaseSuspended, LastActivityAt: now.Add(-8 * 24 * time.Hour).Format(time.RFC3339Nano)},
	}
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: resource.Name, Namespace: resource.Namespace}}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha1.CodexSession{}).WithObjects(resource, pvc).Build()
	controller := &CodexSessionController{Client: kubeClient, RetentionPeriod: 7 * 24 * time.Hour, Now: func() time.Time { return now }}
	if _, err := controller.Reconcile(context.Background(), reconcileRequest(resource)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := kubeClient.Get(context.Background(), types.NamespacedName{Namespace: resource.Namespace, Name: resource.Name}, &v1alpha1.CodexSession{}); err == nil {
		t.Fatal("expired CodexSession was not deleted")
	}
	if err := kubeClient.Get(context.Background(), types.NamespacedName{Namespace: resource.Namespace, Name: resource.Name}, &corev1.PersistentVolumeClaim{}); err == nil {
		t.Fatal("expired session PVC was not deleted")
	}
}

func TestControllerKeepsRecentlyActiveBusySession(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	resource := &v1alpha1.CodexSession{
		ObjectMeta: metav1.ObjectMeta{Name: "session-1", Namespace: "codex-system", CreationTimestamp: metav1.NewTime(now.Add(-8 * 24 * time.Hour))},
		Spec:       reconcileSpec(),
		Status:     v1alpha1.CodexSessionStatus{Phase: v1alpha1.PhaseBusy, LastActivityAt: now.Add(-time.Minute).Format(time.RFC3339Nano)},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha1.CodexSession{}).WithObjects(resource).Build()
	controller := &CodexSessionController{Client: kubeClient, Builder: kubernetes.ResourceBuilder{RunnerImage: "runner:v1.0.0"}, RetentionPeriod: 7 * 24 * time.Hour, Now: func() time.Time { return now }}
	if _, err := controller.Reconcile(context.Background(), reconcileRequest(resource)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := kubeClient.Get(context.Background(), types.NamespacedName{Namespace: resource.Namespace, Name: resource.Name}, &v1alpha1.CodexSession{}); err != nil {
		t.Fatalf("busy CodexSession was deleted: %v", err)
	}
}

func reconcileRequest(resource *v1alpha1.CodexSession) reconcile.Request {
	return reconcile.Request{NamespacedName: types.NamespacedName{Namespace: resource.Namespace, Name: resource.Name}}
}

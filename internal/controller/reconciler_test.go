package controller

import (
	"context"
	"testing"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

type fakeResourceClient struct {
	pvc        *corev1.PersistentVolumeClaim
	pod        *corev1.Pod
	createdPVC int
	createdPod int
	deletedPod int
}

func (f *fakeResourceClient) GetPVC(context.Context, string, string) (*corev1.PersistentVolumeClaim, error) {
	if f.pvc == nil {
		return nil, ErrNotFound
	}
	return f.pvc, nil
}
func (f *fakeResourceClient) CreatePVC(_ context.Context, pvc *corev1.PersistentVolumeClaim) error {
	f.pvc = pvc
	f.createdPVC++
	return nil
}
func (f *fakeResourceClient) GetPod(context.Context, string, string) (*corev1.Pod, error) {
	if f.pod == nil {
		return nil, ErrNotFound
	}
	return f.pod, nil
}
func (f *fakeResourceClient) CreatePod(_ context.Context, pod *corev1.Pod) error {
	f.pod = pod
	f.createdPod++
	return nil
}
func (f *fakeResourceClient) DeletePod(context.Context, string, string) error {
	f.pod = nil
	f.deletedPod++
	return nil
}

func reconcileSpec() v1alpha1.CodexSessionSpec {
	return v1alpha1.CodexSessionSpec{
		ConversationID: "conversation-1",
		DesiredState:   v1alpha1.DesiredStateActive,
		Mode:           v1alpha1.ModeChat,
		SessionData:    v1alpha1.SessionData{Size: "1Gi", Retain: true},
		Execution:      v1alpha1.Execution{ApprovalPolicy: "user"},
	}
}

func TestReconcilerCreatesResourcesThenReportsReady(t *testing.T) {
	client := &fakeResourceClient{}
	r := Reconciler{Client: client}
	result, err := r.Reconcile(context.Background(), "codex-system", "session-1", reconcileSpec())
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if result.Phase != v1alpha1.PhaseStarting || client.createdPVC != 1 || client.createdPod != 1 {
		t.Fatalf("first reconcile = %+v, created pvc=%d pod=%d", result, client.createdPVC, client.createdPod)
	}
	client.pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	result, err = r.Reconcile(context.Background(), "codex-system", "session-1", reconcileSpec())
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if result.Phase != v1alpha1.PhaseReady || client.createdPVC != 1 || client.createdPod != 1 {
		t.Fatalf("second reconcile = %+v, created pvc=%d pod=%d", result, client.createdPVC, client.createdPod)
	}
}

func TestReconcilerSuspendedDeletesOnlyPod(t *testing.T) {
	client := &fakeResourceClient{}
	r := Reconciler{Client: client}
	_, err := r.Reconcile(context.Background(), "codex-system", "session-1", reconcileSpec())
	if err != nil {
		t.Fatal(err)
	}
	spec := reconcileSpec()
	spec.DesiredState = v1alpha1.DesiredStateSuspended
	result, err := r.Reconcile(context.Background(), "codex-system", "session-1", spec)
	if err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if result.Phase != v1alpha1.PhaseSuspended || client.deletedPod != 1 || client.pvc == nil {
		t.Fatalf("suspend = %+v, deleted=%d pvc=%v", result, client.deletedPod, client.pvc)
	}
}

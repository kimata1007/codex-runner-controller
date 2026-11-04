package kubernetes

import (
	"testing"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

func testSessionSpec() v1alpha1.CodexSessionSpec {
	return v1alpha1.CodexSessionSpec{
		ConversationID: "conversation-1",
		DesiredState:   v1alpha1.DesiredStateActive,
		SessionData:    v1alpha1.SessionData{Size: "10Gi", Retain: true},
		Execution:      v1alpha1.Execution{ApprovalPolicy: "user"},
	}
}

func TestResourceBuilderBuildsPVC(t *testing.T) {
	pvc, err := (ResourceBuilder{}).BuildPVC("codex-system", "session-1", testSessionSpec())
	if err != nil {
		t.Fatalf("build pvc: %v", err)
	}
	if pvc.Name != "session-1" || pvc.Namespace != "codex-system" {
		t.Fatalf("unexpected pvc identity: %s/%s", pvc.Namespace, pvc.Name)
	}
	quantity := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if got := quantity.String(); got != "10Gi" {
		t.Fatalf("storage request = %s, want 10Gi", got)
	}
	if pvc.Spec.VolumeMode != nil {
		t.Fatalf("volume mode should use the cluster default")
	}
}

func TestResourceBuilderBuildsPodFromSession(t *testing.T) {
	spec := testSessionSpec()
	spec.Repository = &v1alpha1.Repository{URL: "https://github.com/example/repo.git", Revision: "main"}
	pod, err := (ResourceBuilder{RunnerImage: "runner:v1.0.0"}).BuildPod("codex-system", "session-1", spec)
	if err != nil {
		t.Fatalf("build pod: %v", err)
	}
	if pod.Spec.Containers[0].Image != "runner:v1.0.0" {
		t.Fatalf("runner image = %s", pod.Spec.Containers[0].Image)
	}
	if pod.Spec.Containers[0].SecurityContext == nil || pod.Spec.Containers[0].SecurityContext.AllowPrivilegeEscalation == nil || *pod.Spec.Containers[0].SecurityContext.AllowPrivilegeEscalation {
		t.Fatal("pod must disable privilege escalation")
	}
	if len(pod.Spec.Volumes) != 1 || pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName != "session-1" {
		t.Fatal("pod must mount the session PVC")
	}
	if len(pod.Spec.Containers[0].Env) != 5 {
		t.Fatalf("environment variable count = %d, want 5", len(pod.Spec.Containers[0].Env))
	}
	if pod.Spec.Containers[0].Env[2].Name != "CODEX_HOME" || pod.Spec.Containers[0].Env[2].Value != DefaultMountPath {
		t.Fatal("CODEX_HOME must point to the persistent session volume")
	}
}

func TestResourceBuilderRequiresPinnedImage(t *testing.T) {
	for _, image := range []string{"", "runner", "runner:latest"} {
		if _, err := (ResourceBuilder{RunnerImage: image}).BuildPod("codex-system", "session-1", testSessionSpec()); err == nil {
			t.Errorf("BuildPod accepted unpinned image %q", image)
		}
	}
}

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
	foundPVC := false
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == "session-1" {
			foundPVC = true
		}
	}
	if !foundPVC {
		t.Fatal("pod must mount the session PVC")
	}
	if len(pod.Spec.Containers[0].Env) != 5 {
		t.Fatalf("environment variable count = %d, want 5", len(pod.Spec.Containers[0].Env))
	}
	if pod.Spec.Containers[0].Env[2].Name != "CODEX_HOME" || pod.Spec.Containers[0].Env[2].Value != DefaultMountPath {
		t.Fatal("CODEX_HOME must point to the persistent session volume")
	}
	if pod.Spec.Containers[0].Ports[0].ContainerPort != RunnerAgentPort {
		t.Fatalf("container port = %d, want %d", pod.Spec.Containers[0].Ports[0].ContainerPort, RunnerAgentPort)
	}
}

func TestResourceBuilderRequiresPinnedImage(t *testing.T) {
	for _, image := range []string{"", "runner", "runner:latest"} {
		if _, err := (ResourceBuilder{RunnerImage: image}).BuildPod("codex-system", "session-1", testSessionSpec()); err == nil {
			t.Errorf("BuildPod accepted unpinned image %q", image)
		}
	}
}

func TestResourceBuilderBuildsRunnerAgentService(t *testing.T) {
	service, err := (ResourceBuilder{}).BuildService("codex-system", "session-1", testSessionSpec())
	if err != nil {
		t.Fatalf("build service: %v", err)
	}
	if service.Spec.Ports[0].Port != RunnerAgentPort || service.Spec.Ports[0].TargetPort.IntValue() != RunnerAgentPort {
		t.Fatalf("service port = %+v", service.Spec.Ports[0])
	}
	if service.Spec.Selector[SessionLabel] != "session-1" {
		t.Fatalf("service selector = %+v", service.Spec.Selector)
	}
}

func TestResourceBuilderInjectsRunnerRuntimeConfiguration(t *testing.T) {
	pod, err := (ResourceBuilder{RunnerImage: "runner:v1.0.0", RunnerEventURL: "http://runner-api:8080/internal/runs", RunnerAgentTokenSecretName: "runner-tokens", RunnerAgentTokenSecretKey: "agent-token", RunnerAPITokenSecretName: "runner-tokens", RunnerAPITokenSecretKey: "api-token", RunnerServiceAccountName: "runner-agent"}).BuildPod("codex-system", "session-1", testSessionSpec())
	if err != nil {
		t.Fatalf("build pod: %v", err)
	}
	container := pod.Spec.Containers[0]
	if pod.Spec.ServiceAccountName != "runner-agent" || pod.Spec.SecurityContext == nil || pod.Spec.SecurityContext.RunAsNonRoot == nil || !*pod.Spec.SecurityContext.RunAsNonRoot {
		t.Fatalf("pod security/service account = %+v / %q", pod.Spec.SecurityContext, pod.Spec.ServiceAccountName)
	}
	if len(container.Env) != 6 {
		t.Fatalf("environment variable count = %d, want 6", len(container.Env))
	}
	var eventURL, agentToken, apiToken string
	for _, env := range container.Env {
		switch env.Name {
		case "RUNNER_EVENT_URL":
			eventURL = env.Value
		case "RUNNER_AGENT_TOKEN":
			agentToken = env.ValueFrom.SecretKeyRef.Name + ":" + env.ValueFrom.SecretKeyRef.Key
		case "RUNNER_API_TOKEN":
			apiToken = env.ValueFrom.SecretKeyRef.Name + ":" + env.ValueFrom.SecretKeyRef.Key
		}
	}
	if eventURL != "http://runner-api:8080/internal/runs" || agentToken != "runner-tokens:agent-token" || apiToken != "runner-tokens:api-token" {
		t.Fatalf("runtime env = %q / %q / %q", eventURL, agentToken, apiToken)
	}
}

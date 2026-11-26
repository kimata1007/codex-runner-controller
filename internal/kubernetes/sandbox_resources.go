package kubernetes

import (
	"fmt"
	"net/url"
	"strings"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	SandboxNamespace    = "codex-sessions"
	SandboxRuntimeClass = "codex-gvisor"
)

func (b ResourceBuilder) BuildSandboxPod(name string, spec v1alpha1.CodexSessionSpec) (*corev1.Pod, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if spec.CredentialsRef != nil || b.RunnerAgentTokenSecretName != "" || b.RunnerAPITokenSecretName != "" ||
		b.CodexAccessTokenSecretName != "" || b.RunnerEventURL != "" || b.RunnerHeartbeatURL != "" ||
		b.RunnerAuthRefreshURL != "" || b.OTLPEndpoint != "" {
		return nil, fmt.Errorf("sandbox pod configuration contains legacy credentials or callbacks")
	}
	// The sandbox receives repository coordinates, never a GitHub credential. Git
	// traffic is rewritten to the identity-bound GitHub proxy by the entrypoint.
	if spec.Repository != nil {
		if spec.Access == nil || spec.Access.GitHub == nil {
			return nil, fmt.Errorf("sandbox repository requires a GitHub access grant")
		}
		u, parseErr := url.Parse(spec.Repository.URL)
		if parseErr != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("sandbox repository must be a canonical GitHub HTTPS URL")
		}
		path := strings.TrimSuffix(strings.TrimPrefix(u.EscapedPath(), "/"), ".git")
		parts := strings.Split(path, "/")
		if len(parts) != 2 || parts[0] != spec.Access.GitHub.Owner || parts[1] != spec.Access.GitHub.Repository {
			return nil, fmt.Errorf("sandbox repository does not match its GitHub access grant")
		}
	}
	pod, err := (ResourceBuilder{RunnerImage: b.RunnerImage, RunnerServiceAccountName: name}).BuildPod(SandboxNamespace, name, spec)
	if err != nil {
		return nil, err
	}
	runtime := SandboxRuntimeClass
	runID := int64(10001)
	pod.Spec.RuntimeClassName = &runtime
	pod.Spec.ReadinessGates = []corev1.PodReadinessGate{{ConditionType: v1alpha1.SandboxRouteReady}}
	pod.Spec.EnableServiceLinks = boolPtr(false)
	pod.Spec.SecurityContext.RunAsUser = &runID
	pod.Spec.SecurityContext.RunAsGroup = &runID
	pod.Spec.SecurityContext.FSGroup = &runID
	agent := &pod.Spec.Containers[0]
	agent.Env = append(agent.Env, corev1.EnvVar{Name: "CODEX_ISOLATED_SESSION", Value: "true"})
	if spec.Access != nil && spec.Access.Model != nil {
		agent.Env = append(agent.Env, corev1.EnvVar{
			Name: "CODEX_MODEL_PROXY_ENABLED", Value: "true",
		})
	}
	if spec.Access != nil && spec.Access.GitHub != nil {
		agent.Env = append(agent.Env, corev1.EnvVar{Name: "CODEX_GITHUB_PROXY_ENABLED", Value: "true"})
	}
	agent.Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m"), corev1.ResourceMemory: resource.MustParse("512Mi"),
			corev1.ResourceEphemeralStorage: resource.MustParse("512Mi")},
		Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("2Gi"),
			corev1.ResourceEphemeralStorage: resource.MustParse("2Gi")},
	}
	tempSize := resource.MustParse("512Mi")
	pod.Spec.Volumes[1].EmptyDir.SizeLimit = &tempSize
	for _, check := range []struct {
		probe *corev1.Probe
		path  string
	}{{agent.ReadinessProbe, "/readyz"}, {agent.LivenessProbe, "/healthz"}} {
		check.probe.ProbeHandler = corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"/runner-agent", "-probe", check.path}}}
	}
	return pod, nil
}

func BuildSandboxServiceAccount(name string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta:                   metav1.ObjectMeta{Namespace: SandboxNamespace, Name: name, Labels: sessionLabels(name)},
		AutomountServiceAccountToken: boolPtr(false),
	}
}

package kubernetes

import (
	"testing"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

func TestSandboxPodExcludesCredentialsAndHasRuntimeLimits(t *testing.T) {
	spec := v1alpha1.CodexSessionSpec{ConversationID: "test", DesiredState: "Active",
		Execution: v1alpha1.Execution{ApprovalPolicy: "never"}, SessionData: v1alpha1.SessionData{Size: "1Gi"}}
	builder := ResourceBuilder{RunnerImage: "example.com/runner:v1.2.3"}
	pod, err := builder.BuildSandboxPod("test", spec)
	if err != nil {
		t.Fatal(err)
	}
	if pod.Namespace != SandboxNamespace || pod.Spec.RuntimeClassName == nil || *pod.Spec.RuntimeClassName != SandboxRuntimeClass ||
		pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken || pod.Spec.HostNetwork ||
		pod.Spec.HostPID || pod.Spec.HostIPC || pod.Spec.EnableServiceLinks == nil || *pod.Spec.EnableServiceLinks {
		t.Fatalf("unsafe sandbox configuration: %+v", pod.Spec)
	}
	if pod.Spec.ServiceAccountName != "test" || *BuildSandboxServiceAccount("test").AutomountServiceAccountToken {
		t.Fatal("sandbox service accounts must be scoped per session and lack token mounts")
	}
	container := pod.Spec.Containers[0]
	for _, name := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory, corev1.ResourceEphemeralStorage} {
		limit, request := container.Resources.Limits[name], container.Resources.Requests[name]
		if limit.IsZero() || request.IsZero() {
			t.Fatalf("missing resource limits for %s", name)
		}
	}
	for _, env := range container.Env {
		if env.ValueFrom != nil {
			t.Fatalf("sandbox environment receives a reference: %s", env.Name)
		}
	}
	if container.ReadinessProbe.Exec == nil || container.LivenessProbe.Exec == nil {
		t.Fatal("sandbox probes must use the local network namespace")
	}
	spec.CredentialsRef = &v1alpha1.SecretReference{Name: "legacy"}
	if _, err := builder.BuildSandboxPod("test", spec); err == nil {
		t.Fatal("sandbox accepted a legacy Secret reference")
	}
}

func TestSandboxPodSelectsModelProxyWithoutCredentials(t *testing.T) {
	spec := v1alpha1.CodexSessionSpec{ConversationID: "proxy-test", DesiredState: v1alpha1.DesiredStateActive,
		Execution:   v1alpha1.Execution{Model: "approved-model", ApprovalPolicy: "never"},
		SessionData: v1alpha1.SessionData{Size: "1Gi"},
		Access: &v1alpha1.SessionAccess{Model: &v1alpha1.ModelPolicy{BudgetMicros: 1000, RequestsPerMinute: 10,
			Models: []v1alpha1.ModelGrant{{Name: "approved-model", MaxInputBytes: 4096, MaxOutputTokens: 512,
				InputMicrosPerMillion: 100, OutputMicrosPerMillion: 200}}}}}
	pod, err := (ResourceBuilder{RunnerImage: "example.com/runner:v1.2.3"}).BuildSandboxPod("proxy-test", spec)
	if err != nil {
		t.Fatal(err)
	}
	for _, env := range pod.Spec.Containers[0].Env {
		if env.Name == "CODEX_MODEL_PROXY_ENABLED" && env.Value == "true" {
			return
		}
		if env.Name == "ACCESS_TOKEN" || env.Name == "RUNNER_API_TOKEN" || env.Name == "RUNNER_AGENT_TOKEN" {
			t.Fatalf("sandbox Pod contains a model or runner credential: %s", env.Name)
		}
	}
	t.Fatal("sandbox Pod did not enable its authorized model proxy")
}

func TestSandboxPodRoutesGrantedRepositoryThroughGitHubProxy(t *testing.T) {
	spec := v1alpha1.CodexSessionSpec{ConversationID: "git-proxy-test", DesiredState: v1alpha1.DesiredStateActive,
		Execution: v1alpha1.Execution{ApprovalPolicy: "never"}, SessionData: v1alpha1.SessionData{Size: "1Gi"},
		Repository: &v1alpha1.Repository{URL: "https://github.com/acme/project.git", Revision: "main"},
		Access:     &v1alpha1.SessionAccess{GitHub: &v1alpha1.GitHubPolicy{Owner: "acme", Repository: "project", BaseBranch: "main"}}}
	pod, err := (ResourceBuilder{RunnerImage: "example.com/runner:v1.2.3"}).BuildSandboxPod("git-proxy-test", spec)
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{}
	for _, variable := range pod.Spec.Containers[0].Env {
		env[variable.Name] = variable.Value
		if variable.ValueFrom != nil {
			t.Fatalf("sandbox environment contains a credential reference: %s", variable.Name)
		}
	}
	if env["CODEX_REPOSITORY_URL"] != spec.Repository.URL || env["CODEX_REPOSITORY_REVISION"] != "main" || env["CODEX_GITHUB_PROXY_ENABLED"] != "true" {
		t.Fatalf("repository was not configured for the GitHub proxy: %#v", env)
	}
	spec.Access.GitHub.Repository = "another-project"
	if _, err := (ResourceBuilder{RunnerImage: "example.com/runner:v1.2.3"}).BuildSandboxPod("git-proxy-test", spec); err == nil {
		t.Fatal("sandbox accepted a repository outside its GitHub grant")
	}
}

func TestSandboxPodRoutesOnlyGrantedPackageRegistriesThroughWebProxy(t *testing.T) {
	spec := v1alpha1.CodexSessionSpec{ConversationID: "package-proxy-test", DesiredState: v1alpha1.DesiredStateActive,
		Execution: v1alpha1.Execution{ApprovalPolicy: "never"}, SessionData: v1alpha1.SessionData{Size: "1Gi"},
		Access: &v1alpha1.SessionAccess{Web: &v1alpha1.WebPolicy{Registries: []string{"go", "pypi"}}}}
	pod, err := (ResourceBuilder{RunnerImage: "example.com/runner:v1.2.3"}).BuildSandboxPod("package-proxy-test", spec)
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{}
	for _, variable := range pod.Spec.Containers[0].Env {
		env[variable.Name] = variable.Value
	}
	if env["GOPROXY"] != "http://198.18.0.12:8080/packages/go" || env["PIP_INDEX_URL"] != "http://198.18.0.12:8080/packages/pypi/simple" || env["NPM_CONFIG_REGISTRY"] != "" {
		t.Fatalf("package registry environment does not match grants: %#v", env)
	}
}

package kubernetes

import (
	"fmt"
	"strings"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	resource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	SessionLabel = "codexrunner.io/session"
	ManagedLabel = "codexrunner.io/managed-by"

	DefaultVolumeName           = "session-data"
	DefaultMountPath            = "/var/lib/codex"
	DefaultRunnerServiceAccount = "codex-runner-agent"
	RunnerAgentPort             = 8081
)

type ResourceBuilder struct {
	RunnerImage                string
	RunnerEventURL             string
	RunnerHeartbeatURL         string
	RunnerAgentTokenSecretName string
	RunnerAgentTokenSecretKey  string
	RunnerAPITokenSecretName   string
	RunnerAPITokenSecretKey    string
	CodexAccessTokenSecretName string
	CodexAccessTokenSecretKey  string
	RunnerServiceAccountName   string
}

func (b ResourceBuilder) BuildPVC(namespace, name string, spec v1alpha1.CodexSessionSpec) (*corev1.PersistentVolumeClaim, error) {
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("build pvc: %w", err)
	}
	quantity, err := resource.ParseQuantity(spec.SessionData.Size)
	if err != nil {
		return nil, fmt.Errorf("parse sessionData.size: %w", err)
	}
	labels := sessionLabels(name)
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: optionalString(spec.SessionData.StorageClassName),
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: quantity},
			},
		},
	}
	return pvc, nil
}

func (b ResourceBuilder) BuildPod(namespace, name string, spec v1alpha1.CodexSessionSpec) (*corev1.Pod, error) {
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("build pod: %w", err)
	}
	runnerImage := b.RunnerImage
	imageName := runnerImage[strings.LastIndex(runnerImage, "/")+1:]
	if !strings.Contains(imageName, ":") || strings.HasSuffix(runnerImage, ":") || strings.HasSuffix(runnerImage, ":latest") {
		return nil, fmt.Errorf("runner image must be explicitly pinned")
	}
	labels := sessionLabels(name)
	env := []corev1.EnvVar{
		{Name: "CODEX_CONVERSATION_ID", Value: spec.ConversationID},
		{Name: "CODEX_APPROVAL_POLICY", Value: spec.Execution.ApprovalPolicy},
		{Name: "CODEX_HOME", Value: DefaultMountPath},
	}
	if spec.Execution.Model != "" {
		env = append(env, corev1.EnvVar{Name: "CODEX_MODEL", Value: spec.Execution.Model})
	}
	if spec.Repository != nil {
		env = append(env,
			corev1.EnvVar{Name: "CODEX_REPOSITORY_URL", Value: spec.Repository.URL},
			corev1.EnvVar{Name: "CODEX_REPOSITORY_REVISION", Value: spec.Repository.Revision},
		)
		if spec.CredentialsRef != nil {
			env = append(env, corev1.EnvVar{
				Name: "GIT_AUTH_TOKEN",
				ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: spec.CredentialsRef.Name},
					Key:                  "token",
				}},
			})
		}
	}
	if b.RunnerEventURL != "" {
		env = append(env, corev1.EnvVar{Name: "RUNNER_EVENT_URL", Value: b.RunnerEventURL})
	}
	if b.RunnerHeartbeatURL != "" {
		env = append(env, corev1.EnvVar{Name: "RUNNER_HEARTBEAT_URL", Value: b.RunnerHeartbeatURL})
	}
	if b.RunnerAgentTokenSecretName != "" {
		key := b.RunnerAgentTokenSecretKey
		if key == "" {
			key = "token"
		}
		env = append(env, corev1.EnvVar{Name: "RUNNER_AGENT_TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: b.RunnerAgentTokenSecretName}, Key: key}}})
	}
	if b.RunnerAPITokenSecretName != "" {
		key := b.RunnerAPITokenSecretKey
		if key == "" {
			key = "api-token"
		}
		env = append(env, corev1.EnvVar{Name: "RUNNER_API_TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: b.RunnerAPITokenSecretName}, Key: key}}})
	}
	if b.CodexAccessTokenSecretName != "" {
		key := b.CodexAccessTokenSecretKey
		if key == "" {
			key = "token"
		}
		env = append(env, corev1.EnvVar{Name: "ACCESS_TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: b.CodexAccessTokenSecretName}, Key: key}}})
	}
	serviceAccountName := b.RunnerServiceAccountName
	if serviceAccountName == "" {
		serviceAccountName = DefaultRunnerServiceAccount
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: corev1.PodSpec{
			RestartPolicy:                corev1.RestartPolicyAlways,
			ServiceAccountName:           serviceAccountName,
			AutomountServiceAccountToken: boolPtr(false),
			SecurityContext:              &corev1.PodSecurityContext{RunAsNonRoot: boolPtr(true), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
			Containers: []corev1.Container{{
				Name:    "runner-agent",
				Image:   runnerImage,
				Command: []string{"/runner-agent-entrypoint"},
				Ports:   []corev1.ContainerPort{{Name: "http", ContainerPort: RunnerAgentPort}},
				Env:     env,
				VolumeMounts: []corev1.VolumeMount{{
					Name: DefaultVolumeName, MountPath: DefaultMountPath,
				}, {Name: "tmp", MountPath: "/tmp"}},
				ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromInt(RunnerAgentPort)}}},
				LivenessProbe:  &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromInt(RunnerAgentPort)}}},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: boolPtr(false),
					ReadOnlyRootFilesystem:   boolPtr(true),
					RunAsNonRoot:             boolPtr(true),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
			}},
			Volumes: []corev1.Volume{
				{Name: DefaultVolumeName, VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: name}}},
				{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			},
		},
	}, nil
}

func (b ResourceBuilder) BuildService(namespace, name string, spec v1alpha1.CodexSessionSpec) (*corev1.Service, error) {
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("build service: %w", err)
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: sessionLabels(name)},
		Spec: corev1.ServiceSpec{
			Selector: sessionLabels(name),
			Ports:    []corev1.ServicePort{{Name: "http", Port: RunnerAgentPort, TargetPort: intstr.FromInt(RunnerAgentPort)}},
		},
	}, nil
}

func sessionLabels(name string) map[string]string {
	return map[string]string{SessionLabel: name, ManagedLabel: "codex-runner-controller"}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func boolPtr(value bool) *bool { return &value }

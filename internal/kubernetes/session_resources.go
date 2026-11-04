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

	DefaultVolumeName = "session-data"
	DefaultMountPath  = "/var/lib/codex"
)

type ResourceBuilder struct {
	RunnerImage string
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
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: corev1.PodSpec{
			RestartPolicy:                corev1.RestartPolicyAlways,
			AutomountServiceAccountToken: boolPtr(false),
			Containers: []corev1.Container{{
				Name:    "runner-agent",
				Image:   runnerImage,
				Command: []string{"/runner-agent"},
				Env:     env,
				VolumeMounts: []corev1.VolumeMount{{
					Name: DefaultVolumeName, MountPath: DefaultMountPath,
				}},
				ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromInt(8080)}}},
				LivenessProbe:  &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromInt(8080)}}},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: boolPtr(false),
					ReadOnlyRootFilesystem:   boolPtr(true),
					RunAsNonRoot:             boolPtr(true),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
			}},
			SecurityContext: &corev1.PodSecurityContext{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
			Volumes:         []corev1.Volume{{Name: DefaultVolumeName, VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: name}}}},
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

package zerotrust

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PodReference struct {
	Namespace string
	Name      string
	UID       types.UID
}

type TrustedWorkloads struct {
	Reader            client.Reader
	Domain            string
	APINamespace      string
	SecurityNamespace string
}

type TrustedWorkload struct {
	Binding CertificateBinding
	Pod     corev1.Pod
}

// Lookup derives the role from a live Pod's ReplicaSet and known Deployment.
// Trusted namespaces must limit Deployment, ReplicaSet, Pod and ServiceAccount
// writes to the platform operators.
func (w TrustedWorkloads) Lookup(ctx context.Context, ref PodReference) (TrustedWorkload, error) {
	if w.Reader == nil || ref.Namespace == "" || ref.Name == "" || !component.MatchString(string(ref.UID)) ||
		(ref.Namespace != w.APINamespace && ref.Namespace != w.SecurityNamespace) {
		return TrustedWorkload{}, ErrAttestationDenied
	}
	key := client.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}
	var pod corev1.Pod
	if err := w.Reader.Get(ctx, key, &pod); err != nil || pod.UID != ref.UID || !runningPod(&pod) {
		return TrustedWorkload{}, ErrAttestationDenied
	}
	podOwner := appsOwner(&pod, "ReplicaSet")
	if podOwner == nil {
		return TrustedWorkload{}, ErrAttestationDenied
	}
	var replicaSet appsv1.ReplicaSet
	key.Name = podOwner.Name
	if err := w.Reader.Get(ctx, key, &replicaSet); err != nil || replicaSet.UID != podOwner.UID ||
		replicaSet.DeletionTimestamp != nil || replicaSet.Spec.Template.Spec.ServiceAccountName != pod.Spec.ServiceAccountName {
		return TrustedWorkload{}, ErrAttestationDenied
	}
	owner := appsOwner(&replicaSet, "Deployment")
	if owner == nil {
		return TrustedWorkload{}, ErrAttestationDenied
	}
	role := w.role(ref.Namespace, owner.Name)
	if role == "" || pod.Spec.ServiceAccountName != owner.Name {
		return TrustedWorkload{}, ErrAttestationDenied
	}
	var deployment appsv1.Deployment
	key.Name = owner.Name
	if err := w.Reader.Get(ctx, key, &deployment); err != nil || deployment.UID != owner.UID ||
		deployment.DeletionTimestamp != nil || deployment.Spec.Template.Spec.ServiceAccountName != pod.Spec.ServiceAccountName {
		return TrustedWorkload{}, ErrAttestationDenied
	}
	var account corev1.ServiceAccount
	if err := w.Reader.Get(ctx, key, &account); err != nil || account.UID == "" || account.DeletionTimestamp != nil {
		return TrustedWorkload{}, ErrAttestationDenied
	}
	id, err := ParseIdentity("spiffe://"+w.Domain+"/"+role, w.Domain)
	if err != nil {
		return TrustedWorkload{}, ErrAttestationDenied
	}
	return TrustedWorkload{Binding: CertificateBinding{Identity: id, PodUID: ref.UID, Namespace: ref.Namespace, PodName: ref.Name}, Pod: pod}, nil
}

func (w TrustedWorkloads) role(namespace, deployment string) string {
	if namespace == w.APINamespace && deployment == "codex-runner-api" {
		return "system/runner-api"
	}
	if namespace != w.SecurityNamespace {
		return ""
	}
	switch deployment {
	case "codex-github-proxy":
		return "proxy/github"
	case "codex-web-proxy":
		return "proxy/web"
	case "codex-model-proxy":
		return "proxy/model"
	default:
		return ""
	}
}

func appsOwner(object metav1.Object, kind string) *metav1.OwnerReference {
	owner := metav1.GetControllerOfNoCopy(object)
	if owner == nil || owner.APIVersion != appsv1.SchemeGroupVersion.String() || owner.Kind != kind ||
		owner.Name == "" || owner.UID == "" {
		return nil
	}
	return owner
}

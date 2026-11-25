package zerotrust

import (
	"context"
	"errors"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const maxPolicyLifetime = 10 * time.Minute

var ErrPolicyDenied = errors.New("session policy denied")

// PolicyStore must use an uncached Kubernetes reader. A missing or unreachable
// object cannot authorize a connection, even if a previous lookup succeeded.
type PolicyStore struct {
	Reader           client.Reader
	Domain           string
	PolicyNamespace  string
	SessionNamespace string
	RuntimeClass     string
}

type SessionAuthorization struct {
	Identity Identity
	Workload v1alpha1.SessionWorkload
	Version  int64
	Expires  time.Time
	Policy   *Policy
}

func (s PolicyStore) Lookup(ctx context.Context, podUID types.UID, now time.Time) (SessionAuthorization, error) {
	if s.Reader == nil || s.PolicyNamespace == "" || s.SessionNamespace == "" || s.RuntimeClass == "" ||
		s.PolicyNamespace == s.SessionNamespace || !component.MatchString(string(podUID)) || now.IsZero() {
		return SessionAuthorization{}, ErrPolicyDenied
	}
	var record v1alpha1.SessionPolicy
	key := client.ObjectKey{Namespace: s.PolicyNamespace, Name: string(podUID)}
	if err := s.Reader.Get(ctx, key, &record); err != nil || record.DeletionTimestamp != nil {
		return SessionAuthorization{}, ErrPolicyDenied
	}
	spec := record.Spec
	w := spec.Workload
	if podUID != w.PodUID || spec.Version < 1 || spec.Revoked || !now.Before(spec.ExpiresAt.Time) ||
		spec.ExpiresAt.After(now.Add(maxPolicyLifetime)) || w.SessionUID == "" || w.ServiceAccountUID == "" ||
		w.NodeUID == "" || len(spec.Grants) == 0 || len(spec.Grants) > 16 {
		return SessionAuthorization{}, ErrPolicyDenied
	}
	id, err := ParseIdentity("spiffe://"+s.Domain+"/session/"+w.SessionName+"/pod/"+string(w.PodUID), s.Domain)
	if err != nil {
		return SessionAuthorization{}, ErrPolicyDenied
	}
	grants := make([]Grant, 0, len(spec.Grants))
	for _, grant := range spec.Grants {
		source, destination := Identity(grant.Source), Identity(grant.Destination)
		if grant.Protocol != "tcp" || (source == id) == (destination == id) {
			return SessionAuthorization{}, ErrPolicyDenied
		}
		grants = append(grants, Grant{Source: source, Destination: destination, Port: grant.Port})
	}
	policy, err := NewPolicy(s.Domain, spec.ExpiresAt.Time, grants)
	if err != nil || !s.liveWorkload(ctx, w) {
		return SessionAuthorization{}, ErrPolicyDenied
	}
	return SessionAuthorization{Identity: id, Workload: w, Version: spec.Version, Expires: spec.ExpiresAt.Time, Policy: policy}, nil
}

func (s PolicyStore) liveWorkload(ctx context.Context, w v1alpha1.SessionWorkload) bool {
	key := client.ObjectKey{Namespace: s.SessionNamespace, Name: w.PodName}
	var pod corev1.Pod
	if err := s.Reader.Get(ctx, key, &pod); err != nil || pod.UID != w.PodUID || !runningPod(&pod) ||
		pod.Spec.NodeName != w.NodeName || pod.Spec.ServiceAccountName != w.ServiceAccountName ||
		pod.Spec.RuntimeClassName == nil || *pod.Spec.RuntimeClassName != s.RuntimeClass ||
		pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken ||
		pod.Spec.HostNetwork || pod.Spec.HostPID || pod.Spec.HostIPC {
		return false
	}
	owner := metav1.GetControllerOfNoCopy(&pod)
	if owner == nil || owner.APIVersion != v1alpha1.GroupVersion.String() || owner.Kind != "CodexSession" ||
		owner.Name != w.SessionName || owner.UID != w.SessionUID {
		return false
	}
	var session v1alpha1.CodexSession
	key.Name = w.SessionName
	if err := s.Reader.Get(ctx, key, &session); err != nil || session.UID != w.SessionUID ||
		session.DeletionTimestamp != nil || session.Spec.DesiredState != v1alpha1.DesiredStateActive {
		return false
	}
	var account corev1.ServiceAccount
	key.Name = w.ServiceAccountName
	if err := s.Reader.Get(ctx, key, &account); err != nil || account.UID != w.ServiceAccountUID ||
		account.DeletionTimestamp != nil || account.AutomountServiceAccountToken == nil || *account.AutomountServiceAccountToken {
		return false
	}
	owner = metav1.GetControllerOfNoCopy(&account)
	if owner == nil || owner.APIVersion != v1alpha1.GroupVersion.String() || owner.Kind != "CodexSession" ||
		owner.Name != w.SessionName || owner.UID != w.SessionUID {
		return false
	}
	return liveNode(ctx, s.Reader, w.NodeName, w.NodeUID)
}

func runningPod(pod *corev1.Pod) bool {
	return pod.DeletionTimestamp == nil && pod.Status.Phase == corev1.PodRunning && pod.Spec.NodeName != ""
}

func liveNode(ctx context.Context, reader client.Reader, name string, uid types.UID) bool {
	var node corev1.Node
	if err := reader.Get(ctx, client.ObjectKey{Name: name}, &node); err != nil || node.UID != uid || node.DeletionTimestamp != nil {
		return false
	}
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

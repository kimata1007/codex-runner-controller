package zerotrust

import (
	"context"
	"errors"
	"testing"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testDomain           = "codex.local"
	testPolicyNamespace  = "codex-security"
	testSessionNamespace = "codex-sessions"
	testRuntimeClass     = "codex-gvisor"
	testSessionIdentity  = Identity("spiffe://codex.local/session/test/pod/session-pod-uid")
	testPodUID           = types.UID("session-pod-uid")
)

func boolPointer(value bool) *bool { return &value }

func testReadyNode(name string, uid types.UID) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, UID: uid},
		Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
	}
}

func policyFixtures(now time.Time) []client.Object {
	const sessionName, sessionUID = "test", "session-uid"
	class := testRuntimeClass
	owner := metav1.OwnerReference{APIVersion: v1alpha1.GroupVersion.String(), Kind: "CodexSession", Name: sessionName,
		UID: sessionUID, Controller: boolPointer(true)}
	w := v1alpha1.SessionWorkload{
		SessionName: sessionName, SessionUID: sessionUID, PodName: "test", PodUID: testPodUID,
		NodeName: "worker-1", NodeUID: "node-1-uid", ServiceAccountName: "test-account", ServiceAccountUID: "account-uid",
	}
	return []client.Object{
		&v1alpha1.SessionPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: string(w.PodUID), Namespace: testPolicyNamespace},
			Spec: v1alpha1.SessionPolicySpec{Workload: w, Version: 1, ExpiresAt: metav1.NewTime(now.Add(4 * time.Minute)),
				Grants: []v1alpha1.NetworkGrant{{Source: string(testSessionIdentity), Destination: "spiffe://codex.local/proxy/model", Protocol: "tcp", Port: 8080}},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: w.PodName, Namespace: testSessionNamespace, UID: w.PodUID, OwnerReferences: []metav1.OwnerReference{owner}},
			Spec: corev1.PodSpec{NodeName: w.NodeName, ServiceAccountName: w.ServiceAccountName,
				AutomountServiceAccountToken: boolPointer(false), RuntimeClassName: &class},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
		&v1alpha1.CodexSession{
			ObjectMeta: metav1.ObjectMeta{Name: w.SessionName, Namespace: testSessionNamespace, UID: w.SessionUID},
			Spec:       v1alpha1.CodexSessionSpec{DesiredState: v1alpha1.DesiredStateActive},
		},
		&corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{Name: w.ServiceAccountName, Namespace: testSessionNamespace, UID: w.ServiceAccountUID,
				OwnerReferences: []metav1.OwnerReference{owner}},
			AutomountServiceAccountToken: boolPointer(false),
		},
		testReadyNode(w.NodeName, w.NodeUID),
	}
}

func newTestKube(t *testing.T, objects []client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{v1alpha1.AddToScheme, corev1.AddToScheme, appsv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func testPolicyStore(reader client.Reader) PolicyStore {
	return PolicyStore{Reader: reader, Domain: testDomain, PolicyNamespace: testPolicyNamespace,
		SessionNamespace: testSessionNamespace, RuntimeClass: testRuntimeClass}
}

func TestPolicyStoreMatchesLivePodAndGrants(t *testing.T) {
	now := time.Now()
	store := testPolicyStore(newTestKube(t, policyFixtures(now)))
	policy, err := store.Lookup(t.Context(), testPodUID, now)
	if err != nil || policy.Identity != testSessionIdentity || policy.Version != 1 {
		t.Fatalf("Lookup returned %+v, %v", policy, err)
	}
	if !policy.Policy.Allows(testSessionIdentity, "spiffe://codex.local/proxy/model", 8080, now) ||
		policy.Policy.Allows(testSessionIdentity, "spiffe://codex.local/proxy/github", 8080, now) {
		t.Fatal("policy did not apply the exact registered grant")
	}
}

func TestPolicyStoreDeniesMissingOrChangedWorkload(t *testing.T) {
	now := time.Now()
	tests := map[string]func([]client.Object) []client.Object{
		"missing policy": func(o []client.Object) []client.Object { return o[1:] },
		"deleted pod":    func(o []client.Object) []client.Object { return append(o[:1], o[2:]...) },
		"new pod same name": func(o []client.Object) []client.Object {
			o[1].SetUID("replacement-pod")
			return o
		},
		"wrong node": func(o []client.Object) []client.Object {
			o[1].(*corev1.Pod).Spec.NodeName = "worker-2"
			return o
		},
		"wrong runtime": func(o []client.Object) []client.Object {
			o[1].(*corev1.Pod).Spec.RuntimeClassName = nil
			return o
		},
		"suspended session": func(o []client.Object) []client.Object {
			o[2].(*v1alpha1.CodexSession).Spec.DesiredState = v1alpha1.DesiredStateSuspended
			return o
		},
		"recreated service account": func(o []client.Object) []client.Object {
			o[3].SetUID("replacement-account")
			return o
		},
		"recreated node": func(o []client.Object) []client.Object {
			o[4].SetUID("replacement-node")
			return o
		},
		"expired policy": func(o []client.Object) []client.Object {
			o[0].(*v1alpha1.SessionPolicy).Spec.ExpiresAt = metav1.NewTime(now)
			return o
		},
		"grant for another session": func(o []client.Object) []client.Object {
			o[0].(*v1alpha1.SessionPolicy).Spec.Grants[0].Source = "spiffe://codex.local/session/test/pod/other-pod"
			return o
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			store := testPolicyStore(newTestKube(t, change(policyFixtures(now))))
			if _, err := store.Lookup(t.Context(), testPodUID, now); !errors.Is(err, ErrPolicyDenied) {
				t.Fatalf("Lookup error = %v, want ErrPolicyDenied", err)
			}
		})
	}
}

type unavailableReader struct{ client.Reader }

func (unavailableReader) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return errors.New("unavailable")
}

func TestPolicyStoreRevocationAndOutage(t *testing.T) {
	now := time.Now()
	kube := newTestKube(t, policyFixtures(now))
	store := testPolicyStore(kube)
	if _, err := store.Lookup(t.Context(), testPodUID, now); err != nil {
		t.Fatal(err)
	}
	var policy v1alpha1.SessionPolicy
	if err := kube.Get(t.Context(), client.ObjectKey{Name: string(testPodUID), Namespace: testPolicyNamespace}, &policy); err != nil {
		t.Fatal(err)
	}
	policy.Spec.Revoked = true
	policy.Spec.Version++
	if err := kube.Update(t.Context(), &policy); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Lookup(t.Context(), testPodUID, now); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("revoked policy error = %v", err)
	}
	store.Reader = unavailableReader{}
	if _, err := store.Lookup(t.Context(), testPodUID, now); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("unavailable policy error = %v", err)
	}
}

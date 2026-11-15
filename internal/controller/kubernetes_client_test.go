package controller

import (
	"context"
	"testing"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCreatePVCRespectsRetentionPolicy(t *testing.T) {
	for _, retain := range []bool{false, true} {
		t.Run(map[bool]string{false: "delete_with_session", true: "retain_after_session_delete"}[retain], func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			if err := v1alpha1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			owner := &v1alpha1.CodexSession{ObjectMeta: metav1.ObjectMeta{
				Name: "session-1", Namespace: "codex-system", UID: types.UID("session-uid"),
			}}
			kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			resourceClient := KubernetesResourceClient{Client: kubeClient, Owner: owner, RetainPVC: retain}
			pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "session-1", Namespace: "codex-system"}}
			if err := resourceClient.CreatePVC(context.Background(), pvc); err != nil {
				t.Fatalf("create PVC: %v", err)
			}
			stored := &corev1.PersistentVolumeClaim{}
			if err := kubeClient.Get(context.Background(), types.NamespacedName{Namespace: "codex-system", Name: "session-1"}, stored); err != nil {
				t.Fatalf("get PVC: %v", err)
			}
			if retain && len(stored.OwnerReferences) != 0 {
				t.Fatalf("retained PVC has owner references: %+v", stored.OwnerReferences)
			}
			if !retain && (len(stored.OwnerReferences) != 1 || stored.OwnerReferences[0].UID != owner.UID) {
				t.Fatalf("non-retained PVC owner references = %+v", stored.OwnerReferences)
			}
		})
	}
}

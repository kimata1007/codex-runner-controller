package zerotrust

import (
	"context"
	"crypto/x509"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestIssuerClientAttestsRunnerDeployment(t *testing.T) {
	server, kube, _, _ := newTestIssuer(t)
	const namespace = "codex-system"
	const deploymentName = "codex-runner-api"
	const proxyPodUID = types.UID("pod-for-runner-api")
	template := corev1.PodTemplateSpec{Spec: corev1.PodSpec{ServiceAccountName: deploymentName}}
	owner := func(name string, uid types.UID, kind string) []metav1.OwnerReference {
		return []metav1.OwnerReference{{APIVersion: appsv1.SchemeGroupVersion.String(), Name: name,
			UID: uid, Kind: kind, Controller: boolPointer(true)}}
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "runner-api-pod", UID: proxyPodUID,
			OwnerReferences: owner("runner-api-replicaset", "runner-rs", "ReplicaSet")},
		Spec:   corev1.PodSpec{NodeName: "worker-1", ServiceAccountName: deploymentName},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	for _, object := range []client.Object{
		pod,
		&appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "runner-api-replicaset", UID: "runner-rs",
				OwnerReferences: owner(deploymentName, "runner-deployment", "Deployment")},
			Spec: appsv1.ReplicaSetSpec{Template: template},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: deploymentName, UID: "runner-deployment"},
			Spec:       appsv1.DeploymentSpec{Template: template},
		},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: deploymentName, UID: "runner-sa"}},
	} {
		if err := kube.Create(t.Context(), object); err != nil {
			t.Fatal(err)
		}
	}
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	token := "proxy-token-1"
	c, err := NewIssuerClient(server.URL, testDomain, roots, func() (string, error) { return token, nil })
	if err != nil {
		t.Fatal(err)
	}
	binding := CertificateBinding{
		Identity: "spiffe://codex.local/system/runner-api", PodUID: proxyPodUID, PodName: pod.Name, Namespace: namespace,
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := c.IssueForBinding(ctx, binding); err != nil {
		t.Fatalf("valid runner-api deployment: %v", err)
	}
	token = "proxy-token-2"
	if _, err := c.IssueForBinding(ctx, binding); err == nil {
		t.Fatal("issued a runner-api certificate to a different node")
	}
	token = "proxy-token-1"
	if err := kube.Delete(ctx, pod); err != nil {
		t.Fatal(err)
	}
	if _, err := c.IssueForBinding(ctx, binding); err == nil {
		t.Fatal("reissued a certificate from a deleted trusted pod")
	}
}

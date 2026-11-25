package zerotrust

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const testNodeAccount = "codex-node-proxy"

type tokenReviewFunc func(context.Context, *authv1.TokenReview) (*authv1.TokenReview, error)

func (f tokenReviewFunc) Create(ctx context.Context, review *authv1.TokenReview, _ metav1.CreateOptions) (*authv1.TokenReview, error) {
	return f(ctx, review)
}

func testProxyPod(name, node string, uid types.UID) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testPolicyNamespace, UID: uid,
			OwnerReferences: []metav1.OwnerReference{{APIVersion: appsv1.SchemeGroupVersion.String(), Kind: "DaemonSet",
				Name: testNodeAccount, UID: "node-proxy-daemon", Controller: boolPointer(true)}}},
		Spec:   corev1.PodSpec{NodeName: node, ServiceAccountName: testNodeAccount},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func testTokenReview(pod, podUID, node, nodeUID string) authv1.TokenReviewStatus {
	return authv1.TokenReviewStatus{
		Authenticated: true, Audiences: []string{NodeTokenAudience},
		User: authv1.UserInfo{
			Username: "system:serviceaccount:" + testPolicyNamespace + ":" + testNodeAccount,
			UID:      "node-proxy-service-account",
			Extra: map[string]authv1.ExtraValue{
				"authentication.kubernetes.io/pod-name":  {pod},
				"authentication.kubernetes.io/pod-uid":   {podUID},
				"authentication.kubernetes.io/node-name": {node},
				"authentication.kubernetes.io/node-uid":  {nodeUID},
			},
		},
	}
}

func writeCAFiles(t *testing.T, path string, ca *x509.Certificate, key *ecdsa.PrivateKey, bundle []byte) {
	t.Helper()
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"ca.crt":     pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw}),
		"ca.key":     pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		"bundle.crt": bundle,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(path, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func newTestIssuer(t *testing.T) (*httptest.Server, client.Client, *x509.Certificate, CAFiles) {
	t.Helper()
	now := time.Now()
	objects := append(policyFixtures(now),
		testReadyNode("worker-2", "node-2-uid"),
		testProxyPod("proxy-1", "worker-1", "proxy-1-uid"),
		testProxyPod("proxy-2", "worker-2", "proxy-2-uid"),
		&appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: testNodeAccount, Namespace: testPolicyNamespace, UID: "node-proxy-daemon"},
			Spec:       appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{ServiceAccountName: testNodeAccount}}},
		},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: testNodeAccount, Namespace: testPolicyNamespace, UID: "node-proxy-service-account"}},
	)
	kube := newTestKube(t, objects)
	reviewer := tokenReviewFunc(func(_ context.Context, r *authv1.TokenReview) (*authv1.TokenReview, error) {
		if len(r.Spec.Audiences) != 1 || r.Spec.Audiences[0] != NodeTokenAudience {
			t.Error("issuer requested an incorrect token audience")
		}
		result := &authv1.TokenReview{}
		switch r.Spec.Token {
		case "proxy-token-1":
			result.Status = testTokenReview("proxy-1", "proxy-1-uid", "worker-1", "node-1-uid")
		case "proxy-token-2":
			result.Status = testTokenReview("proxy-2", "proxy-2-uid", "worker-2", "node-2-uid")
		case "stale-node-token":
			result.Status = testTokenReview("proxy-1", "proxy-1-uid", "worker-1", "old-node-uid")
		case "wrong-audience-token":
			result.Status = testTokenReview("proxy-1", "proxy-1-uid", "worker-1", "node-1-uid")
			result.Status.Audiences = []string{"kubernetes.default.svc"}
		}
		return result, nil
	})
	nodes := NodeAuthenticator{Tokens: reviewer, Reader: kube, Namespace: testPolicyNamespace, ServiceAccount: testNodeAccount, DaemonSet: testNodeAccount}
	ca, key := makeTestCA(t, now)
	files := CAFiles{Directory: t.TempDir(), Domain: testDomain}
	writeCAFiles(t, files.Directory, ca, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw}))
	server := httptest.NewUnstartedServer(NewIssuerHandler(files, nodes, testPolicyStore(kube),
		TrustedWorkloads{Reader: kube, Domain: testDomain, APINamespace: "codex-system", SecurityNamespace: testPolicyNamespace}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server, kube, ca, files
}

func TestIssuerHTTPAttestsCrossNodeAndRevokedPolicy(t *testing.T) {
	server, kube, ca, _ := newTestIssuer(t)
	csr, _ := makeTestCSR(t)
	issue := func(token string, uid types.UID, wantStatus int) []byte {
		t.Helper()
		body, err := json.Marshal(certificateRequest{PodUID: uid, CSR: csr})
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(t.Context(), "POST", server.URL+issuancePath, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != wantStatus {
			t.Fatalf("issuance status = %d, want %d: %s (%v)", res.StatusCode, wantStatus, data, err)
		}
		return data
	}
	for _, tc := range []struct {
		token string
		uid   types.UID
		want  Identity
	}{
		{"proxy-token-1", testPodUID, testSessionIdentity},
		{"proxy-token-1", "", "spiffe://codex.local/node/node-1-uid"},
	} {
		var response certificateResponse
		if err := json.Unmarshal(issue(tc.token, tc.uid, http.StatusOK), &response); err != nil {
			t.Fatal(err)
		}
		cert, err := x509.ParseCertificate(response.Certificate)
		if err != nil {
			t.Fatal(err)
		}
		id, err := peerIdentity(cert, testDomain)
		if err != nil || id != tc.want || cert.CheckSignatureFrom(ca) != nil {
			t.Fatalf("issued certificate identity = %s, want %s: %v", id, tc.want, err)
		}
	}
	issue("proxy-token-2", testPodUID, http.StatusForbidden)
	issue("stale-node-token", testPodUID, http.StatusUnauthorized)
	issue("wrong-audience-token", testPodUID, http.StatusUnauthorized)
	issue("session-account-token", testPodUID, http.StatusUnauthorized)
	issue("proxy-token-1", "other-session-uid", http.StatusForbidden)
	var policy v1alpha1.SessionPolicy
	if err := kube.Get(t.Context(), client.ObjectKey{Namespace: testPolicyNamespace, Name: string(testPodUID)}, &policy); err != nil {
		t.Fatal(err)
	}
	policy.Spec.Revoked = true
	policy.Spec.Version++
	if err := kube.Update(t.Context(), &policy); err != nil {
		t.Fatal(err)
	}
	issue("proxy-token-1", testPodUID, http.StatusForbidden)
}

func TestCAFilesRotatesWithOverlappingBundle(t *testing.T) {
	now := time.Now()
	directory := t.TempDir()
	files := CAFiles{Directory: directory, Domain: testDomain}
	oldRoot, oldKey := makeTestCA(t, now)
	newRoot, newKey := makeTestCA(t, now)
	rootPEM := func(ca *x509.Certificate) []byte {
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw})
	}
	for _, name := range []string{"one", "two"} {
		if err := os.Mkdir(filepath.Join(directory, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	writeCAFiles(t, filepath.Join(directory, "one"), oldRoot, oldKey, rootPEM(oldRoot))
	writeCAFiles(t, filepath.Join(directory, "two"), newRoot, newKey, append(rootPEM(oldRoot), rootPEM(newRoot)...))
	for _, tc := range []struct {
		revision string
		root     *x509.Certificate
	}{{"one", oldRoot}, {"two", newRoot}} {
		staged := filepath.Join(directory, "..next")
		if err := os.Symlink(tc.revision, staged); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(staged, filepath.Join(directory, "..data")); err != nil {
			t.Fatal(err)
		}
		issuer, err := files.Issuer(now)
		if err != nil || !issuer.CA.Equal(tc.root) {
			t.Fatalf("mounted revision %s: unexpected active root, %v", tc.revision, err)
		}
	}
	bundle, err := files.TrustBundle()
	if err != nil || !bytes.Equal(bundle, append(rootPEM(oldRoot), rootPEM(newRoot)...)) {
		t.Fatalf("overlapping trust bundle not published: %v", err)
	}
}

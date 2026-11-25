package zerotrust

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIssuerClientCreatesDistinctCredentialsAndMutualTLS(t *testing.T) {
	issuer, _, _, _ := newTestIssuer(t)
	bootstrapRoots := x509.NewCertPool()
	bootstrapRoots.AddCert(issuer.Certificate())
	reads := 0
	client, err := NewIssuerClient(issuer.URL, testDomain, bootstrapRoots, func() (string, error) {
		reads++
		return "proxy-token-1\n", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	nodeID := Identity("spiffe://codex.local/node/node-1-uid")
	node, err := client.Issue(t.Context(), "", nodeID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.Issue(t.Context(), testPodUID, testSessionIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if reads != 2 || string(node.Leaf.RawSubjectPublicKeyInfo) == string(session.Leaf.RawSubjectPublicKeyInfo) {
		t.Fatal("issuance did not reload the projected token and separate the workload keys")
	}
	roots, err := client.TrustBundle(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Issue(t.Context(), testPodUID, "spiffe://codex.local/session/other/pod/session-pod-uid"); err == nil {
		t.Fatal("issuer client accepted a different identity for the same Pod UID")
	}
	serverConfig, err := TLSConfig(node, roots, testDomain, testSessionIdentity, true)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := PeerIdentity(*r.TLS, testDomain)
		if err != nil || id != testSessionIdentity {
			t.Errorf("received %s: %v", id, err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	server.TLS = serverConfig
	server.StartTLS()
	t.Cleanup(server.Close)
	connect := func(cert tls.Certificate, authorities *x509.CertPool, destination Identity) error {
		t.Helper()
		config, err := TLSConfig(cert, authorities, testDomain, destination, false)
		if err != nil {
			t.Fatal(err)
		}
		transport := &http.Transport{TLSClientConfig: config}
		defer transport.CloseIdleConnections()
		httpClient := &http.Client{Transport: transport}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := httpClient.Do(req)
		if err != nil {
			return err
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNoContent {
			t.Fatalf("mutual TLS response status = %d", res.StatusCode)
		}
		return nil
	}
	if err := connect(session, roots, nodeID); err != nil {
		t.Fatalf("authorized mutual TLS failed: %v", err)
	}
	if err := connect(node, roots, nodeID); err == nil {
		t.Fatal("server accepted the wrong source identity")
	}
	if err := connect(session, roots, "spiffe://codex.local/node/node-2-uid"); err == nil {
		t.Fatal("client accepted the wrong destination identity")
	}
	if err := connect(session, bootstrapRoots, nodeID); err == nil {
		t.Fatal("client accepted an untrusted workload CA")
	}
}

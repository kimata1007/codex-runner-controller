package zerotrust

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"math/big"
	"net/url"
	"testing"
	"time"
)

func makeTestCA(t *testing.T, now time.Time) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	return ca, caKey
}

func makeTestCSR(t *testing.T) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := url.Parse("spiffe://codex.local/system/runner-api")
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{URIs: []*url.URL{claimed}}, key)
	if err != nil {
		t.Fatal(err)
	}
	return csr, key
}

func TestIssuer(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	ca, caKey := makeTestCA(t, now)
	csr, _ := makeTestCSR(t)
	id := Identity("spiffe://codex.local/session/test/pod/uid-1")
	issuer := Issuer{Domain: "codex.local", CA: ca, Signer: caKey}
	attest := func(context.Context) (Attestation, error) {
		return Attestation{Identity: id, Deadline: now.Add(time.Minute)}, nil
	}
	der, err := issuer.Issue(context.Background(), attest, csr, now)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	if len(leaf.URIs) != 1 || leaf.URIs[0].String() != string(id) || !leaf.NotAfter.Equal(now.Add(time.Minute)) || leaf.IsCA {
		t.Fatal("unexpected issued certificate")
	}
	if err := leaf.CheckSignatureFrom(ca); err != nil {
		t.Fatal(err)
	}
	deny := func(context.Context) (Attestation, error) { return Attestation{}, errors.New("denied") }
	if _, err := issuer.Issue(context.Background(), deny, csr, now); !errors.Is(err, ErrAttestationDenied) {
		t.Fatal("issued certificate without attestation")
	}
	if _, err := issuer.Issue(context.Background(), nil, csr, now); err == nil {
		t.Fatal("issued certificate without attestor")
	}
	csr[len(csr)-1] ^= 1
	if _, err := issuer.Issue(context.Background(), attest, csr, now); !errors.Is(err, ErrInvalidCSR) {
		t.Fatal("accepted invalid CSR signature")
	}
}

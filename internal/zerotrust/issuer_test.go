package zerotrust

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"math/big"
	"testing"
	"time"
)

type testAttestor struct {
	err error
}

func (a testAttestor) AuthorizeCertificate(context.Context, Identity) error {
	return a.err
}

func TestIssuer(t *testing.T) {
	now := time.Now().Truncate(time.Second)
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
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if err != nil {
		t.Fatal(err)
	}
	id := Identity("spiffe://codex.local/session/test/pod/uid-1")
	issuer := Issuer{Domain: "codex.local", CA: ca, Signer: caKey, Attestor: testAttestor{}}
	der, err := issuer.Issue(context.Background(), id, csr, now)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	if len(leaf.URIs) != 1 || leaf.URIs[0].String() != string(id) || leaf.NotAfter.After(now.Add(5*time.Minute)) || leaf.IsCA {
		t.Fatal("unexpected issued certificate")
	}
	if err := leaf.CheckSignatureFrom(ca); err != nil {
		t.Fatal(err)
	}
	issuer.Attestor = testAttestor{err: errors.New("denied")}
	if _, err := issuer.Issue(context.Background(), id, csr, now); err == nil {
		t.Fatal("issued certificate without attestation")
	}
	issuer.Attestor = nil
	if _, err := issuer.Issue(context.Background(), id, csr, now); err == nil {
		t.Fatal("issued certificate without attestor")
	}
}

package zerotrust

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net/url"
	"time"
)

// Attestor must independently verify that the authenticated requester is
// authorized to obtain this exact identity. A caller-supplied Pod UID is not
// attestation. Implementations must reject deleted or reassigned workloads.
type Attestor interface {
	AuthorizeCertificate(context.Context, Identity) error
}

type Issuer struct {
	Domain   string
	CA       *x509.Certificate
	Signer   crypto.Signer
	Attestor Attestor
}

func (i Issuer) Issue(ctx context.Context, identity Identity, csrDER []byte, now time.Time) ([]byte, error) {
	if i.CA == nil || i.Signer == nil || i.Attestor == nil || !i.CA.IsCA ||
		i.CA.KeyUsage&x509.KeyUsageCertSign == 0 || now.IsZero() ||
		now.Before(i.CA.NotBefore) || !now.Before(i.CA.NotAfter) {
		return nil, errors.New("issuer unavailable")
	}
	if _, err := ParseIdentity(string(identity), i.Domain); err != nil {
		return nil, err
	}
	if len(csrDER) == 0 || len(csrDER) > 16384 {
		return nil, errors.New("invalid CSR size")
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil || csr.CheckSignature() != nil {
		return nil, errors.New("invalid CSR signature")
	}
	if err := i.Attestor.AuthorizeCertificate(ctx, identity); err != nil {
		return nil, errors.New("workload attestation failed")
	}
	uri, err := url.Parse(string(identity))
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	serial.Add(serial, big.NewInt(1))
	expires := now.Add(5 * time.Minute)
	if i.CA.NotAfter.Before(expires) {
		expires = i.CA.NotAfter
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{},
		NotBefore: now.Add(-time.Second),
		NotAfter: expires,
		KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		URIs: []*url.URL{uri},
	}
	return x509.CreateCertificate(rand.Reader, template, i.CA, csr.PublicKey, i.Signer)
}

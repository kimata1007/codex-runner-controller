package zerotrust

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net/url"
	"time"
)

const DefaultCertificateTTL = 5 * time.Minute

var (
	ErrAttestationDenied = errors.New("workload attestation failed")
	ErrInvalidCSR        = errors.New("invalid certificate signing request")
)

// Attestation is derived by a trusted verifier. The certificate may not
// outlive the authorization that allowed its issuance.
type Attestation struct {
	Identity Identity
	Deadline time.Time
}

type Issuer struct {
	Domain string
	CA     *x509.Certificate
	Signer crypto.Signer
	TTL    time.Duration
}

func (i Issuer) Issue(ctx context.Context, attest func(context.Context) (Attestation, error), csrDER []byte, now time.Time) ([]byte, error) {
	if i.CA == nil || i.Signer == nil || attest == nil || !i.CA.IsCA ||
		i.CA.KeyUsage&x509.KeyUsageCertSign == 0 || now.IsZero() ||
		now.Before(i.CA.NotBefore) || !now.Before(i.CA.NotAfter) {
		return nil, errors.New("issuer unavailable")
	}
	ttl := i.TTL
	if ttl == 0 {
		ttl = DefaultCertificateTTL
	}
	if ttl < time.Second || ttl > 10*time.Minute {
		return nil, errors.New("invalid certificate lifetime")
	}
	if len(csrDER) == 0 || len(csrDER) > 16384 {
		return nil, ErrInvalidCSR
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil || csr.CheckSignature() != nil {
		return nil, ErrInvalidCSR
	}
	switch key := csr.PublicKey.(type) {
	case *ecdsa.PublicKey:
		if key.Curve != elliptic.P256() {
			return nil, ErrInvalidCSR
		}
	case ed25519.PublicKey:
	default:
		return nil, ErrInvalidCSR
	}
	assertion, err := attest(ctx)
	if err != nil {
		return nil, ErrAttestationDenied
	}
	if _, err := ParseIdentity(string(assertion.Identity), i.Domain); err != nil {
		return nil, ErrAttestationDenied
	}
	expires := now.Add(ttl)
	if !assertion.Deadline.IsZero() && assertion.Deadline.Before(expires) {
		expires = assertion.Deadline
	}
	if i.CA.NotAfter.Before(expires) {
		expires = i.CA.NotAfter
	}
	if !now.Before(expires.Truncate(time.Second)) || ctx.Err() != nil {
		return nil, ErrAttestationDenied
	}
	uri, _ := url.Parse(string(assertion.Identity))
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	serial.Add(serial, big.NewInt(1))
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{},
		NotBefore:             now,
		NotAfter:              expires,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		URIs:                  []*url.URL{uri},
	}
	return x509.CreateCertificate(rand.Reader, template, i.CA, csr.PublicKey, i.Signer)
}

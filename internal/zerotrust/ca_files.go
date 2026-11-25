package zerotrust

import (
	"bytes"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

const maxTrustBundleBytes = 64 << 10

// CAFiles loads the active signing root and the overlapping public trust
// bundle from the same mounted Secret revision.
type CAFiles struct {
	Directory string
	Domain    string
	TTL       time.Duration
}

func (f CAFiles) open() (*os.Root, error) {
	if !filepath.IsAbs(f.Directory) {
		return nil, errors.New("CA directory must be absolute")
	}
	root, err := os.OpenRoot(filepath.Join(f.Directory, "..data"))
	if errors.Is(err, os.ErrNotExist) {
		return os.OpenRoot(f.Directory)
	}
	return root, err
}

func (f CAFiles) Issuer(now time.Time) (Issuer, error) {
	root, err := f.open()
	if err != nil {
		return Issuer{}, err
	}
	defer root.Close()
	certPEM, err := readCAFile(root, "ca.crt")
	if err != nil {
		return Issuer{}, err
	}
	keyPEM, err := readCAFile(root, "ca.key")
	if err != nil {
		return Issuer{}, err
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	clear(keyPEM)
	if err != nil || len(pair.Certificate) != 1 {
		return Issuer{}, errors.New("invalid signing root key pair")
	}
	ca, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || !ca.IsCA || ca.KeyUsage&x509.KeyUsageCertSign == 0 ||
		ca.CheckSignatureFrom(ca) != nil || now.Before(ca.NotBefore) || !now.Before(ca.NotAfter) {
		return Issuer{}, errors.New("invalid signing root certificate")
	}
	signer, ok := pair.PrivateKey.(crypto.Signer)
	if !ok {
		return Issuer{}, errors.New("invalid signing key")
	}
	_, roots, err := loadTrustBundle(root)
	if err != nil {
		return Issuer{}, err
	}
	if _, err := ca.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}); err != nil {
		return Issuer{}, errors.New("signing root is missing from the trust bundle")
	}
	return Issuer{Domain: f.Domain, CA: ca, Signer: signer, TTL: f.TTL}, nil
}

func (f CAFiles) TrustBundle() ([]byte, error) {
	root, err := f.open()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	bundle, _, err := loadTrustBundle(root)
	return bundle, err
}

func loadTrustBundle(root *os.Root) ([]byte, *x509.CertPool, error) {
	bundle, err := readCAFile(root, "bundle.crt")
	if err != nil {
		return nil, nil, err
	}
	roots, err := parseTrustBundle(bundle)
	if err != nil {
		return nil, nil, err
	}
	return bundle, roots, nil
}

func readCAFile(root *os.Root, name string) ([]byte, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxTrustBundleBytes {
		return nil, errors.New("invalid issuer file")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxTrustBundleBytes+1))
	if err != nil || len(data) > maxTrustBundleBytes {
		return nil, errors.New("unable to read issuer file")
	}
	return data, nil
}

func parseTrustBundle(bundle []byte) (*x509.CertPool, error) {
	if len(bundle) == 0 || len(bundle) > maxTrustBundleBytes {
		return nil, errors.New("invalid trust bundle size")
	}
	roots := x509.NewCertPool()
	rest := bytes.TrimSpace(bundle)
	if len(rest) == 0 {
		return nil, errors.New("empty trust bundle")
	}
	for len(rest) > 0 {
		if !bytes.HasPrefix(rest, []byte("-----BEGIN CERTIFICATE-----")) {
			return nil, errors.New("trust bundle contains non-certificate data")
		}
		block, tail := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return nil, errors.New("invalid trust bundle PEM")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !cert.IsCA || cert.KeyUsage&x509.KeyUsageCertSign == 0 || cert.CheckSignatureFrom(cert) != nil {
			return nil, errors.New("invalid trust bundle root")
		}
		roots.AddCert(cert)
		rest = bytes.TrimSpace(tail)
	}
	return roots, nil
}

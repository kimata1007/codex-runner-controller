package zerotrust

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

// IssuerClient uses a separately configured HTTPS trust root to bootstrap
// credentials. The projected node token is read anew for every issuance.
type IssuerClient struct {
	endpoint string
	domain   string
	http     *http.Client
	token    func() (string, error)
}

func NewIssuerClient(endpoint, domain string, roots *x509.CertPool, token func() (string, error)) (*IssuerClient, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" ||
		u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" || roots == nil || token == nil {
		return nil, errors.New("invalid identity issuer HTTPS configuration")
	}
	if _, err := ParseIdentity("spiffe://"+domain+"/system/issuer", domain); err != nil {
		return nil, err
	}
	transport := &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
		TLSHandshakeTimeout: 3 * time.Second,
		IdleConnTimeout:     30 * time.Second,
		MaxIdleConnsPerHost: 8,
		MaxConnsPerHost:     32,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, RootCAs: roots.Clone()},
	}
	return &IssuerClient{
		endpoint: strings.TrimSuffix(endpoint, "/"), domain: domain, token: token,
		http: &http.Client{Transport: transport, Timeout: issuanceAPITimeout + time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func (c *IssuerClient) TrustBundle(ctx context.Context) (*x509.CertPool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+trustBundlePath, nil)
	if err != nil {
		return nil, err
	}
	bundle, err := c.read(req, maxTrustBundleBytes)
	if err != nil {
		return nil, err
	}
	return parseTrustBundle(bundle)
}

// Issue creates a key locally and verifies the returned certificate against
// the independently obtained trust bundle and the exact expected identity.
func (c *IssuerClient) Issue(ctx context.Context, podUID types.UID, expected Identity) (tls.Certificate, error) {
	if _, err := ParseIdentity(string(expected), c.domain); err != nil {
		return tls.Certificate{}, err
	}
	if podUID != "" && !strings.HasSuffix(string(expected), "/pod/"+string(podUID)) {
		return tls.Certificate{}, errors.New("expected identity does not match Pod UID")
	}
	if podUID == "" && !strings.HasPrefix(string(expected), "spiffe://"+c.domain+"/node/") {
		return tls.Certificate{}, errors.New("node certificate requires a node identity")
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, private)
	if err != nil {
		return tls.Certificate{}, err
	}
	data, err := json.Marshal(certificateRequest{PodUID: podUID, CSR: csr})
	if err != nil {
		return tls.Certificate{}, err
	}
	token, err := c.token()
	token = strings.TrimSpace(token)
	if err != nil || token == "" || len(token) > 8192 || strings.ContainsAny(token, " \t\r\n,") {
		return tls.Certificate{}, errors.New("projected node credential unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+issuancePath, bytes.NewReader(data))
	if err != nil {
		return tls.Certificate{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	response, err := c.read(req, maxIssueBodyBytes)
	if err != nil {
		return tls.Certificate{}, err
	}
	var issued certificateResponse
	if err := json.Unmarshal(response, &issued); err != nil {
		return tls.Certificate{}, errors.New("invalid certificate response")
	}
	leaf, err := x509.ParseCertificate(issued.Certificate)
	if err != nil {
		return tls.Certificate{}, errors.New("invalid issued certificate")
	}
	id, err := peerIdentity(leaf, c.domain)
	if err != nil || id != expected || !public.Equal(leaf.PublicKey) ||
		leaf.KeyUsage&x509.KeyUsageDigitalSignature == 0 || leaf.NotAfter.Sub(leaf.NotBefore) > 10*time.Minute ||
		!slices.Contains(leaf.ExtKeyUsage, x509.ExtKeyUsageServerAuth) || !slices.Contains(leaf.ExtKeyUsage, x509.ExtKeyUsageClientAuth) {
		return tls.Certificate{}, errors.New("issued certificate does not match workload credentials")
	}
	roots, err := c.TrustBundle(ctx)
	if err != nil {
		return tls.Certificate{}, err
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		return tls.Certificate{}, errors.New("issued certificate failed trust validation")
	}
	return tls.Certificate{Certificate: [][]byte{issued.Certificate}, PrivateKey: private, Leaf: leaf}, nil
}

func (c *IssuerClient) read(req *http.Request, maximum int64) ([]byte, error) {
	response, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("identity issuer unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("identity issuer rejected request")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil || int64(len(data)) > maximum {
		return nil, errors.New("invalid identity issuer response")
	}
	return data, nil
}

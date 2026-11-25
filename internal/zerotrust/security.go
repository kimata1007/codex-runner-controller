// Package zerotrust provides the identity and authorization primitives shared by
// the node and service proxies. It does not attest network sources by itself.
package zerotrust

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

var component = regexp.MustCompile("^[a-z0-9][a-z0-9-]{0,127}$")
var trustDomain = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)

// Identity is a canonical URI SAN under the configured trust domain.
type Identity string

func ParseIdentity(raw, domain string) (Identity, error) {
	u, err := url.Parse(raw)
	if err != nil || len(domain) > 253 || !trustDomain.MatchString(domain) || u.Scheme != "spiffe" || u.Host != domain ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		u.RawPath != "" || u.Opaque != "" || u.String() != raw {
		return "", errors.New("invalid workload identity")
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 2 && len(parts) != 4 {
		return "", errors.New("invalid identity path")
	}
	for _, p := range parts {
		if !component.MatchString(p) {
			return "", errors.New("invalid identity component")
		}
	}
	if len(parts) == 4 {
		if parts[0] != "session" || parts[2] != "pod" {
			return "", errors.New("invalid session identity")
		}
	} else if parts[0] != "system" && parts[0] != "proxy" && parts[0] != "node" {
		return "", errors.New("invalid identity kind")
	}
	return Identity(raw), nil
}

func peerIdentity(cert *x509.Certificate, domain string) (Identity, error) {
	if cert == nil || cert.IsCA || len(cert.URIs) != 1 ||
		len(cert.DNSNames)+len(cert.IPAddresses)+len(cert.EmailAddresses) != 0 {
		return "", errors.New("exactly one URI SAN required")
	}
	return ParseIdentity(cert.URIs[0].String(), domain)
}

// PeerIdentity must only be called on a connection verified by TLSConfig.
func PeerIdentity(state tls.ConnectionState, domain string) (Identity, error) {
	if !state.HandshakeComplete || len(state.PeerCertificates) == 0 {
		return "", errors.New("missing authenticated peer")
	}
	return peerIdentity(state.PeerCertificates[0], domain)
}

// TLSConfig verifies trust chain, key usage and exact expected workload identity.
// The caller must use an authorized expected identity, never a peer-supplied one.
// Server mode supports an explicit identity check for each inbound connection.
func TLSConfig(cert tls.Certificate, roots *x509.CertPool, domain string, expected Identity, server bool) (*tls.Config, error) {
	if _, err := ParseIdentity(string(expected), domain); err != nil {
		return nil, err
	}
	return tlsConfig(cert, roots, domain, server, func(id Identity) error {
		if id != expected {
			return errors.New("unexpected peer identity")
		}
		return nil
	})
}

func tlsConfig(cert tls.Certificate, roots *x509.CertPool, domain string, server bool, authorize func(Identity) error) (*tls.Config, error) {
	if roots == nil || len(cert.Certificate) == 0 || cert.PrivateKey == nil || authorize == nil {
		return nil, errors.New("missing TLS credentials")
	}
	cfg := &tls.Config{
		MinVersion:             tls.VersionTLS13,
		MaxVersion:             tls.VersionTLS13,
		Certificates:           []tls.Certificate{cert},
		SessionTicketsDisabled: true,
	}
	usage := x509.ExtKeyUsageServerAuth
	if server {
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
		cfg.ClientCAs = roots
		usage = x509.ExtKeyUsageClientAuth
	} else {
		// SPIFFE URI SANs are not DNS names. Verify the full chain and exact
		// URI in VerifyConnection rather than using DNS hostname verification.
		cfg.InsecureSkipVerify = true // verification is replaced, not disabled
		cfg.RootCAs = roots
	}
	cfg.VerifyConnection = func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return errors.New("missing peer certificate")
		}
		leaf := cs.PeerCertificates[0]
		if leaf.KeyUsage&x509.KeyUsageDigitalSignature == 0 || !slices.Contains(leaf.ExtKeyUsage, usage) {
			return errors.New("missing required certificate usage")
		}
		intermediates := x509.NewCertPool()
		for _, c := range cs.PeerCertificates[1:] {
			intermediates.AddCert(c)
		}
		if _, err := leaf.Verify(x509.VerifyOptions{
			Roots: roots, Intermediates: intermediates,
			KeyUsages: []x509.ExtKeyUsage{usage},
		}); err != nil {
			return fmt.Errorf("verify peer chain: %w", err)
		}
		id, err := peerIdentity(leaf, domain)
		if err != nil {
			return err
		}
		return authorize(id)
	}
	return cfg, nil
}

// Grant is an exact directed connection grant. Wildcards are not supported.
type Grant struct {
	Source      Identity `json:"source"`
	Destination Identity `json:"destination"`
	Port        uint16   `json:"port"`
}

// Policy is an immutable, expiring snapshot. Construct a new snapshot on update.
type Policy struct {
	expires time.Time
	grants  map[Grant]struct{}
}

func NewPolicy(domain string, expires time.Time, grants []Grant) (*Policy, error) {
	if domain == "" || expires.IsZero() {
		return nil, errors.New("invalid policy metadata")
	}
	p := &Policy{expires: expires, grants: make(map[Grant]struct{}, len(grants))}
	for _, g := range grants {
		if _, err := ParseIdentity(string(g.Source), domain); err != nil {
			return nil, err
		}
		if _, err := ParseIdentity(string(g.Destination), domain); err != nil {
			return nil, err
		}
		if g.Port == 0 {
			return nil, errors.New("invalid destination port")
		}
		if _, exists := p.grants[g]; exists {
			return nil, errors.New("duplicate network grant")
		}
		p.grants[g] = struct{}{}
	}
	return p, nil
}

func (p *Policy) Allows(source, destination Identity, port uint16, now time.Time) bool {
	if p == nil || now.IsZero() || !now.Before(p.expires) {
		return false
	}
	_, ok := p.grants[Grant{Source: source, Destination: destination, Port: port}]
	return ok
}

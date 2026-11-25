package zerotrust

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base32"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	relayConnectTimeout   = 5 * time.Second
	relayAuthorizeTimeout = 2 * time.Second
	defaultPolicyInterval = time.Second
	defaultRelayIdle      = 2 * time.Minute
)

// Route parameters come from the trusted interceptor and route registry.
type Route struct {
	Source      CertificateBinding
	Destination Identity
	Port        uint16
	Address     string
}

type LocalTarget struct {
	Destination CertificateBinding
	Port        uint16
	Connect     func(context.Context, Identity) (net.Conn, error)
}

type RouteAuthority interface {
	AuthorizeRoute(context.Context, Identity, Identity, uint16) (RoutePermission, error)
}

type RelayCredentials interface {
	Certificate(context.Context, CertificateBinding) (tls.Certificate, error)
}

type Relay struct {
	Domain         string
	Authority      RouteAuthority
	Credentials    RelayCredentials
	Roots          func(context.Context) (*x509.CertPool, error)
	Dialer         net.Dialer
	policyInterval time.Duration
	idleTimeout    time.Duration
}

// TargetName selects the receiving listener route through the TLS handshake.
// The name by itself conveys no authority.
func TargetName(id Identity, port uint16) string {
	input := string(id) + ":" + strconv.Itoa(int(port))
	digest := sha256.Sum256([]byte(input))
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:])) + ".zt"
}

func (r *Relay) allowed(ctx context.Context, source, destination Identity, port uint16) (RoutePermission, error) {
	if r.Authority == nil {
		return RoutePermission{}, ErrPolicyDenied
	}
	ctx, cancel := context.WithTimeout(ctx, relayAuthorizeTimeout)
	defer cancel()
	allow, err := r.Authority.AuthorizeRoute(ctx, source, destination, port)
	if err != nil || ctx.Err() != nil || allow.Version < 1 || !time.Now().Before(allow.ExpiresAt) {
		return RoutePermission{}, ErrPolicyDenied
	}
	return allow, nil
}

func (r *Relay) Inbound(ctx context.Context, transport net.Conn, resolve func(string) (LocalTarget, bool)) error {
	defer transport.Close()
	if r.Credentials == nil || r.Roots == nil || resolve == nil {
		return ErrPolicyDenied
	}
	var target LocalTarget
	var source Identity
	var cert tls.Certificate
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
		MaxVersion: tls.VersionTLS13,
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			t, ok := resolve(hello.ServerName)
			if !ok || t.Connect == nil || hello.ServerName != TargetName(t.Destination.Identity, t.Port) {
				return nil, ErrPolicyDenied
			}
			target = t
			roots, err := r.Roots(hello.Context())
			if err != nil {
				return nil, err
			}
			cert, err = r.Credentials.Certificate(hello.Context(), target.Destination)
			if err != nil {
				return nil, err
			}
			return tlsConfig(cert, roots, r.Domain, true, func(id Identity) error {
				if _, err := r.allowed(hello.Context(), id, target.Destination.Identity, target.Port); err != nil {
					return err
				}
				source = id
				return nil
			})
		},
	}
	inbound := tls.Server(transport, cfg)
	handshake, cancel := context.WithTimeout(ctx, relayConnectTimeout)
	err := inbound.HandshakeContext(handshake)
	cancel()
	if err != nil {
		return err
	}
	permission, err := r.allowed(ctx, source, target.Destination.Identity, target.Port)
	if err != nil {
		return err
	}
	expires, err := certificateDeadline(inbound, cert)
	if err != nil {
		return err
	}
	dialCtx, cancel := context.WithDeadline(ctx, expires)
	dialCtx, stop := context.WithTimeout(dialCtx, relayConnectTimeout)
	upstream, err := target.Connect(dialCtx, source)
	stop()
	cancel()
	if err != nil {
		return err
	}
	defer upstream.Close()
	return r.bridge(ctx, inbound, upstream, source, target.Destination.Identity, target.Port, permission, expires)
}

func (r *Relay) Outbound(ctx context.Context, local net.Conn, route Route) error {
	defer local.Close()
	if r.Credentials == nil || r.Roots == nil {
		return ErrPolicyDenied
	}
	if _, err := r.allowed(ctx, route.Source.Identity, route.Destination, route.Port); err != nil {
		return err
	}
	handshake, cancel := context.WithTimeout(ctx, relayConnectTimeout)
	defer cancel()
	cert, err := r.Credentials.Certificate(handshake, route.Source)
	if err != nil {
		return err
	}
	roots, err := r.Roots(handshake)
	if err != nil {
		return err
	}
	cfg, err := TLSConfig(cert, roots, r.Domain, route.Destination, false)
	if err != nil {
		return err
	}
	cfg.ServerName = TargetName(route.Destination, route.Port)
	dialer := tls.Dialer{NetDialer: &r.Dialer, Config: cfg}
	remote, err := dialer.DialContext(handshake, "tcp", route.Address)
	if err != nil {
		return err
	}
	cancel()
	defer remote.Close()
	permission, err := r.allowed(ctx, route.Source.Identity, route.Destination, route.Port)
	if err != nil {
		return err
	}
	expires, err := certificateDeadline(remote.(*tls.Conn), cert)
	if err != nil {
		return err
	}
	return r.bridge(ctx, local, remote, route.Source.Identity, route.Destination, route.Port, permission, expires)
}

func certificateDeadline(conn *tls.Conn, local tls.Certificate) (time.Time, error) {
	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 || local.Leaf == nil {
		return time.Time{}, errors.New("missing relay certificate")
	}
	peerUntil := state.PeerCertificates[0].NotAfter
	if local.Leaf.NotAfter.Before(peerUntil) {
		peerUntil = local.Leaf.NotAfter
	}
	if !time.Now().Before(peerUntil) {
		return time.Time{}, errors.New("relay certificate expired")
	}
	return peerUntil, nil
}

type activityWriter struct {
	dst  net.Conn
	last *atomic.Int64
}

func (w activityWriter) Write(b []byte) (int, error) {
	n, err := w.dst.Write(b)
	if n > 0 {
		w.last.Store(time.Now().UnixNano())
	}
	return n, err
}

func (r *Relay) bridge(ctx context.Context, a, b net.Conn, source, destination Identity, port uint16, permit RoutePermission, certExpiry time.Time) error {
	var lastActivity atomic.Int64
	lastActivity.Store(time.Now().UnixNano())
	idle := r.idleTimeout
	if idle <= 0 {
		idle = defaultRelayIdle
	}
	setDeadline := func(expires time.Time) {
		if certExpiry.Before(expires) {
			expires = certExpiry
		}
		_ = a.SetDeadline(expires)
		_ = b.SetDeadline(expires)
	}
	setDeadline(permit.ExpiresAt)
	var wg sync.WaitGroup
	done := make(chan error, 2)
	copyStream := func(dst, src net.Conn) {
		defer wg.Done()
		_, err := io.Copy(activityWriter{dst: dst, last: &lastActivity}, src)
		if err == nil {
			if half, ok := dst.(interface{ CloseWrite() error }); ok {
				err = half.CloseWrite()
			} else {
				err = dst.Close()
			}
		}
		done <- err
	}
	wg.Add(2)
	go copyStream(a, b)
	go copyStream(b, a)
	defer func() {
		_ = a.SetDeadline(time.Now())
		_ = b.SetDeadline(time.Now())
		_ = a.Close()
		_ = b.Close()
		wg.Wait()
	}()
	interval := r.policyInterval
	if interval <= 0 {
		interval = defaultPolicyInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for completed := 0; completed < 2; {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Since(time.Unix(0, lastActivity.Load())) >= idle {
				return errors.New("relay idle timeout")
			}
			if !time.Now().Before(permit.ExpiresAt) || !time.Now().Before(certExpiry) {
				return ErrPolicyDenied
			}
			current, err := r.allowed(ctx, source, destination, port)
			if err != nil || current.Version < permit.Version {
				return ErrPolicyDenied
			}
			permit = current
			setDeadline(permit.ExpiresAt)
		case err := <-done:
			if err != nil {
				return err
			}
			completed++
		}
	}
	return nil
}

package zerotrust

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// Route is supplied exclusively by trusted control-plane configuration.
// It must never be constructed from untrusted HTTP headers or destination input.
type Route struct {
	Source      Identity
	Destination Identity
	Port        uint16
	Address     string
}

// PolicySource supplies current immutable policy snapshots.
type PolicySource interface {
	Current() *Policy
}

// Relay forwards one stream after verifying its authenticated peer and exact
// route. The downstream socket must be reachable only via the trusted network
// boundary. It is not safe to use this as a standalone CNI implementation.
type Relay struct {
	Domain   string
	Policies PolicySource
	Dialer   net.Dialer
}

func (r *Relay) allowed(route Route) bool {
	return r.Policies != nil && r.Policies.Current().Allows(route.Source, route.Destination, route.Port, time.Now())
}

func (r *Relay) Inbound(ctx context.Context, conn *tls.Conn, route Route) error {
	defer conn.Close()
	if !r.allowed(route) {
		return errors.New("route denied")
	}
	handshake, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := conn.HandshakeContext(handshake)
	cancel()
	if err != nil {
		return err
	}
	id, err := PeerIdentity(conn.ConnectionState(), r.Domain)
	if err != nil || id != route.Source {
		return errors.New("source identity mismatch")
	}
	if !r.allowed(route) {
		return errors.New("route denied after authentication")
	}
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	upstream, err := r.Dialer.DialContext(dialCtx, "tcp", route.Address)
	cancel()
	if err != nil {
		return err
	}
	defer upstream.Close()
	return r.bridge(ctx, conn, upstream, route)
}

// Outbound accepts only a source socket already attributed to the route by a
// trusted interceptor. The TLS configuration must verify route.Destination.
func (r *Relay) Outbound(ctx context.Context, local net.Conn, route Route, cfg *tls.Config) error {
	defer local.Close()
	if cfg == nil || !r.allowed(route) {
		return errors.New("route denied")
	}
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	dialer := tls.Dialer{NetDialer: &r.Dialer, Config: cfg}
	remote, err := dialer.DialContext(dialCtx, "tcp", route.Address)
	cancel()
	if err != nil {
		return err
	}
	defer remote.Close()
	state := remote.(*tls.Conn).ConnectionState()
	id, err := PeerIdentity(state, r.Domain)
	if err != nil || id != route.Destination || !r.allowed(route) {
		return errors.New("destination identity or policy mismatch")
	}
	return r.bridge(ctx, local, remote, route)
}

func (r *Relay) bridge(ctx context.Context, a, b net.Conn, route Route) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	done := make(chan error, 2)
	copyStream := func(dst, src net.Conn) {
		defer wg.Done()
		_, err := io.Copy(dst, src)
		if half, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = half.CloseWrite()
		} else {
			_ = dst.Close()
		}
		done <- err
	}
	wg.Add(2)
	go copyStream(a, b)
	go copyStream(b, a)
	defer func() {
		_ = a.Close()
		_ = b.Close()
		wg.Wait()
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	completed := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if !r.allowed(route) {
				return errors.New("route authorization revoked")
			}
		case err := <-done:
			if err != nil {
				return err
			}
			completed++
			if completed == 2 {
				return nil
			}
		}
	}
}

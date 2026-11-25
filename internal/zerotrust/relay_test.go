package zerotrust

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

type testRelayAuthority struct {
	revoked atomic.Bool
	checks  atomic.Int32
}

func (a *testRelayAuthority) AuthorizeRoute(_ context.Context, source, destination Identity, port uint16) (RoutePermission, error) {
	a.checks.Add(1)
	if a.revoked.Load() || source != "spiffe://codex.local/system/runner-api" ||
		destination != testSessionIdentity || port != ControlPort {
		return RoutePermission{}, ErrPolicyDenied
	}
	return RoutePermission{Version: 1, ExpiresAt: time.Now().Add(time.Minute)}, nil
}

type testRelayCredentials map[Identity]tls.Certificate

func (c testRelayCredentials) Certificate(_ context.Context, binding CertificateBinding) (tls.Certificate, error) {
	certificate, exists := c[binding.Identity]
	if !exists {
		return tls.Certificate{}, ErrAttestationDenied
	}
	return certificate, nil
}

func TestRelayRevokesAnAuthenticatedStream(t *testing.T) {
	now := time.Now()
	root, signer := makeTestCA(t, now)
	issuer := Issuer{Domain: testDomain, CA: root, Signer: signer}
	credentials := make(testRelayCredentials)
	source := Identity("spiffe://codex.local/system/runner-api")
	for _, id := range []Identity{source, testSessionIdentity} {
		csr, key := makeTestCSR(t)
		der, err := issuer.Issue(t.Context(), func(context.Context) (Attestation, error) {
			return Attestation{Identity: id}, nil
		}, csr, now)
		if err != nil {
			t.Fatal(err)
		}
		leaf, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		credentials[id] = tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
	}
	pool := x509.NewCertPool()
	pool.AddCert(root)
	bundle := func(context.Context) (*x509.CertPool, error) { return pool, nil }
	sender, receiver := &testRelayAuthority{}, &testRelayAuthority{}
	outbound := &Relay{Domain: testDomain, Authority: sender, Credentials: credentials, Roots: bundle, policyInterval: 30 * time.Millisecond}
	inbound := &Relay{Domain: testDomain, Authority: receiver, Credentials: credentials, Roots: bundle, policyInterval: 30 * time.Millisecond}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	target := LocalTarget{
		Destination: CertificateBinding{Identity: testSessionIdentity, PodUID: testPodUID}, Port: ControlPort,
		Connect: func(ctx context.Context, authenticated Identity) (net.Conn, error) {
			if authenticated != source {
				return nil, ErrPolicyDenied
			}
			echo, service := net.Pipe()
			go func() {
				defer echo.Close()
				_, _ = io.Copy(echo, echo)
			}()
			return service, nil
		},
	}
	incoming := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			incoming <- err
			return
		}
		incoming <- inbound.Inbound(ctx, connection, func(name string) (LocalTarget, bool) {
			return target, name == TargetName(target.Destination.Identity, target.Port)
		})
	}()
	user, intercepted := net.Pipe()
	defer user.Close()
	if err := user.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	outgoing := make(chan error, 1)
	go func() {
		outgoing <- outbound.Outbound(ctx, intercepted, Route{
			Source: CertificateBinding{Identity: source}, Destination: testSessionIdentity, Port: ControlPort, Address: listener.Addr().String(),
		})
	}()
	if _, err := io.WriteString(user, "hello"); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 5)
	if _, err := io.ReadFull(user, response); err != nil || string(response) != "hello" {
		t.Fatalf("relayed response was %q, %v", response, err)
	}
	if sender.checks.Load() < 2 || receiver.checks.Load() < 2 {
		t.Fatal("both peers must authorize the route around the handshake")
	}
	receiver.revoked.Store(true)
	select {
	case err := <-incoming:
		if !errors.Is(err, ErrPolicyDenied) {
			t.Fatalf("incoming error = %v, want policy denial", err)
		}
	case <-ctx.Done():
		t.Fatal("revoked stream remained open")
	}
	select {
	case <-outgoing:
	case <-ctx.Done():
		t.Fatal("revocation did not terminate the originating stream")
	}
}

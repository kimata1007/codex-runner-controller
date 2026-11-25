package zerotrust

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

type testCertificateIssuer func(context.Context, types.UID, Identity) (tls.Certificate, error)

func (i testCertificateIssuer) IssueForBinding(ctx context.Context, binding CertificateBinding) (tls.Certificate, error) {
	return i(ctx, binding.PodUID, binding.Identity)
}

func TestCertificateManagerCoalescesAndRenews(t *testing.T) {
	var count atomic.Int32
	issuer := testCertificateIssuer(func(_ context.Context, _ types.UID, _ Identity) (tls.Certificate, error) {
		count.Add(1)
		now := time.Now()
		return tls.Certificate{Leaf: &x509.Certificate{NotBefore: now, NotAfter: now.Add(time.Minute)}}, nil
	})
	binding := CertificateBinding{Identity: testSessionIdentity, PodUID: testPodUID}
	manager := &CertificateManager{issuer: issuer, keys: make(map[CertificateBinding]*certificateEntry)}
	var callers sync.WaitGroup
	for range 15 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			if _, err := manager.Certificate(t.Context(), binding); err != nil {
				t.Error(err)
			}
		}()
	}
	callers.Wait()
	if count.Load() != 1 {
		t.Fatalf("issued %d certificates for one binding", count.Load())
	}
	manager.keys[binding].refreshAt.Store(time.Now().Add(-time.Second).UnixNano())
	if _, err := manager.Certificate(t.Context(), binding); err != nil || count.Load() != 2 {
		t.Fatalf("certificate renewal failed: %v, attempts %d", err, count.Load())
	}
}

func TestCertificateManagerDiscardsCertificateOnConcurrentRemoval(t *testing.T) {
	started, unblock := make(chan struct{}), make(chan struct{})
	issuer := testCertificateIssuer(func(ctx context.Context, _ types.UID, _ Identity) (tls.Certificate, error) {
		close(started)
		select {
		case <-unblock:
			now := time.Now()
			return tls.Certificate{Leaf: &x509.Certificate{NotBefore: now, NotAfter: now.Add(time.Minute)}}, nil
		case <-ctx.Done():
			return tls.Certificate{}, ctx.Err()
		}
	})
	manager := &CertificateManager{issuer: issuer, keys: make(map[CertificateBinding]*certificateEntry)}
	binding := CertificateBinding{Identity: testSessionIdentity, PodUID: testPodUID}
	done := make(chan error, 1)
	go func() {
		_, err := manager.Certificate(t.Context(), binding)
		done <- err
	}()
	<-started
	manager.Forget(binding)
	close(unblock)
	if err := <-done; !errors.Is(err, ErrAttestationDenied) {
		t.Fatalf("concurrent issuance returned %v, want revoked binding denial", err)
	}
}

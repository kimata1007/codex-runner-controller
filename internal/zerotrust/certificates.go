package zerotrust

import (
	"context"
	"crypto/tls"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

type CertificateBinding struct {
	Identity  Identity
	PodUID    types.UID
	Namespace string
	PodName   string
}

type certificateIssuer interface {
	IssueForBinding(context.Context, CertificateBinding) (tls.Certificate, error)
}

// CertificateManager holds private keys only in the Node Proxy process. The
// caller must derive bindings from verified routes, and revoke them on removal.
type CertificateManager struct {
	issuer certificateIssuer
	mu     sync.Mutex
	keys   map[CertificateBinding]*certificateEntry
}

type certificateEntry struct {
	lock      chan struct{}
	revoked   atomic.Bool
	refreshAt atomic.Int64
	cert      tls.Certificate
}

const maxCachedWorkloads = 2048

func NewCertificateManager(issuer *IssuerClient) *CertificateManager {
	return &CertificateManager{issuer: issuer, keys: make(map[CertificateBinding]*certificateEntry)}
}

func (m *CertificateManager) Certificate(ctx context.Context, binding CertificateBinding) (tls.Certificate, error) {
	if err := ctx.Err(); err != nil {
		return tls.Certificate{}, err
	}
	m.mu.Lock()
	entry := m.keys[binding]
	if entry == nil && len(m.keys) < maxCachedWorkloads {
		entry = &certificateEntry{lock: make(chan struct{}, 1)}
		m.keys[binding] = entry
	}
	m.mu.Unlock()
	if entry == nil {
		return tls.Certificate{}, errors.New("workload credential capacity reached")
	}
	return m.certificate(ctx, binding, entry)
}

func (m *CertificateManager) certificate(ctx context.Context, binding CertificateBinding, entry *certificateEntry) (tls.Certificate, error) {
	select {
	case entry.lock <- struct{}{}:
		defer func() { <-entry.lock }()
	case <-ctx.Done():
		return tls.Certificate{}, ctx.Err()
	}
	if entry.revoked.Load() {
		return tls.Certificate{}, ErrAttestationDenied
	}
	now := time.Now()
	if entry.cert.Leaf != nil && now.Before(time.Unix(0, entry.refreshAt.Load())) {
		return entry.cert, nil
	}
	cert, err := m.issuer.IssueForBinding(ctx, binding)
	if err != nil {
		entry.cert = tls.Certificate{}
		return tls.Certificate{}, err
	}
	if entry.revoked.Load() || cert.Leaf == nil || !time.Now().Before(cert.Leaf.NotAfter) {
		return tls.Certificate{}, ErrAttestationDenied
	}
	lead := min(time.Minute, cert.Leaf.NotAfter.Sub(cert.Leaf.NotBefore)/3)
	entry.cert = cert
	entry.refreshAt.Store(cert.Leaf.NotAfter.Add(-lead).UnixNano())
	return cert, nil
}

// Forget drops the cache entry and invalidates any renewal already in progress.
func (m *CertificateManager) Forget(binding CertificateBinding) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry := m.keys[binding]; entry != nil {
		entry.revoked.Store(true)
		delete(m.keys, binding)
	}
}

// Renew refreshes active credentials before expiry while the proxy is running.
// It exits after in-flight renewals stop when ctx is canceled.
func (m *CertificateManager) Renew(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var running sync.WaitGroup
	defer running.Wait()
	slots := make(chan struct{}, 8)
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			m.mu.Lock()
			for binding, entry := range m.keys {
				refreshAt := entry.refreshAt.Load()
				if refreshAt != 0 && now.UnixNano() >= refreshAt {
					select {
					case slots <- struct{}{}:
						running.Add(1)
						go func() {
							defer running.Done()
							defer func() { <-slots }()
							_, _ = m.certificate(ctx, binding, entry)
						}()
					default:
					}
				}
			}
			m.mu.Unlock()
		}
	}
}

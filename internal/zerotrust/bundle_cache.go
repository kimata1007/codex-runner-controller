package zerotrust

import (
	"context"
	"crypto/x509"
	"time"
)

type bundleCache struct {
	issuer *IssuerClient
	lock   chan struct{}
	pool   *x509.CertPool
	until  time.Time
}

func (c *bundleCache) load(ctx context.Context) (*x509.CertPool, error) {
	select {
	case c.lock <- struct{}{}:
		defer func() { <-c.lock }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if c.pool != nil && time.Now().Before(c.until) {
		return c.pool, nil
	}
	pool, err := c.issuer.TrustBundle(ctx)
	if err != nil {
		return nil, err
	}
	c.pool, c.until = pool, time.Now().Add(15*time.Second)
	return pool, nil
}

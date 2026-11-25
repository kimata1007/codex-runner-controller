package zerotrust

import (
	"context"
	"strings"
	"time"
)

const (
	ControlPort uint16 = 8081
	ProxyPort   uint16 = 8080
)

type RoutePermission struct {
	Version   int64
	ExpiresAt time.Time
}

// AuthorizeRoute enforces the fixed topology before consulting the current
// Pod policy. Policy changes cannot introduce Session-to-Session routes.
func (s PolicyStore) AuthorizeRoute(ctx context.Context, source, destination Identity, port uint16) (RoutePermission, error) {
	session, ok := topologySession(s.Domain, source, destination, port)
	if !ok {
		return RoutePermission{}, ErrPolicyDenied
	}
	record, err := s.Session(ctx, session)
	if err != nil || !record.Policy.Allows(source, destination, port, time.Now()) {
		return RoutePermission{}, ErrPolicyDenied
	}
	return RoutePermission{Version: record.Version, ExpiresAt: record.Expires}, nil
}

func topologySession(domain string, source, destination Identity, port uint16) (Identity, bool) {
	prefix := "spiffe://" + domain
	if port == ControlPort && source == Identity(prefix+"/system/runner-api") && strings.HasPrefix(string(destination), prefix+"/session/") {
		return destination, true
	}
	if port == ProxyPort && strings.HasPrefix(string(source), prefix+"/session/") {
		switch destination {
		case Identity(prefix + "/proxy/github"), Identity(prefix + "/proxy/web"), Identity(prefix + "/proxy/model"):
			return source, true
		}
	}
	return "", false
}

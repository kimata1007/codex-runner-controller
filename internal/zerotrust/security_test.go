package zerotrust

import (
	"testing"
	"time"
)

func TestPolicy(t *testing.T) {
	const domain = "codex.local"
	a := Identity("spiffe://codex.local/session/a/pod/uid-1")
	b := Identity("spiffe://codex.local/session/b/pod/uid-2")
	dest := Identity("spiffe://codex.local/proxy/github")
	now := time.Now()
	p, err := NewPolicy(domain, now.Add(time.Minute), []Grant{{Source: a, Destination: dest, Port: 443}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		source, destination Identity
		port uint16
		at time.Time
		want bool
	}{
		{a, dest, 443, now, true},
		{b, dest, 443, now, false},
		{dest, a, 443, now, false},
		{a, dest, 80, now, false},
		{a, dest, 443, now.Add(time.Minute), false},
		{a, b, 443, now, false},
	} {
		if got := p.Allows(tc.source, tc.destination, tc.port, tc.at); got != tc.want {
			t.Errorf("Allows(%s, %s, %d) = %t, want %t", tc.source, tc.destination, tc.port, got, tc.want)
		}
	}
	var missing *Policy
	if missing.Allows(a, dest, 443, now) {
		t.Fatal("missing policy allowed traffic")
	}
}

func TestParseIdentity(t *testing.T) {
	for _, raw := range []string{
		"spiffe://other.local/session/a/pod/uid-1",
		"spiffe://codex.local/session/a/pod/",
		"spiffe://codex.local/session/a/pod/uid-1?admin=true",
		"spiffe://codex.local/session/a/pod/%75id-1",
		"spiffe://codex.local/session/a",
		"spiffe://codex.local/proxy/github#fragment",
		"spiffe://codex.local/proxy/../github",
		"spiffe://codex.local/proxy/*",
	} {
		if _, err := ParseIdentity(raw, "codex.local"); err == nil {
			t.Errorf("accepted invalid identity %q", raw)
		}
	}
}

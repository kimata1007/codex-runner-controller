package zerotrust

import (
	"net/netip"
	"net/url"
	"testing"
)

func TestPublicIP(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.100.100.200", "192.168.1.1", "172.16.0.1", "0.0.0.0", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "64:ff9b::a00:1", "2001:db8::1"} {
		if publicIP(netip.MustParseAddr(raw)) {
			t.Errorf("accepted non-public address %s", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "2606:4700:4700::1111"} {
		if !publicIP(netip.MustParseAddr(raw)) {
			t.Errorf("rejected public address %s", raw)
		}
	}
}

func TestWebURL(t *testing.T) {
	for _, raw := range []string{"http://example.com", "https://localhost", "https://api.svc.cluster.local", "https://127.0.0.1", "https://user:pass@example.com", "https://example.com:8443", "file:///etc/passwd", "https://example.com/#fragment"} {
		u, err := url.Parse(raw)
		if err == nil && validateWebURL(u) == nil {
			t.Errorf("accepted prohibited URL %s", raw)
		}
	}
	u, err := url.Parse("https://example.com/docs?q=go")
	if err != nil || validateWebURL(u) != nil {
		t.Fatal("public HTTPS URL rejected")
	}
}

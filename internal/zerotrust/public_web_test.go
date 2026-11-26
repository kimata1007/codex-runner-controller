package zerotrust

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
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

func TestPublicWebRejectsMixedDNSAnswers(t *testing.T) {
	for _, ips := range [][]netip.Addr{
		nil,
		{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.0.0.1")},
		{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("169.254.169.254")},
	} {
		if publicDNSAnswers(ips) {
			t.Errorf("accepted mixed or empty DNS answers: %v", ips)
		}
	}
	if !publicDNSAnswers([]netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("1.1.1.1")}) {
		t.Fatal("rejected public DNS answers")
	}
}

type webRoundTrip func(*http.Request) (*http.Response, error)

func (f webRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPublicWebRevalidatesRedirectsAndRegistryHosts(t *testing.T) {
	for _, test := range []struct {
		name      string
		location  string
		allowHost func(string) bool
	}{
		{name: "private address", location: "https://127.0.0.1/admin"},
		{name: "metadata address", location: "https://169.254.169.254/latest/meta-data/"},
		{name: "registry redirect escape", location: "https://storage.googleapis.com/module.zip", allowHost: func(host string) bool { return host == "proxy.golang.org" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			web := NewPublicWeb()
			web.client.Transport = webRoundTrip(func(request *http.Request) (*http.Response, error) {
				calls++
				if calls > 1 {
					t.Fatalf("followed prohibited redirect to %s", request.URL)
				}
				return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{test.location}}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
			})
			_, _, _, err := web.FetchBounded(context.Background(), "https://proxy.golang.org/module/@v/list", 1024, test.allowHost)
			if err == nil || calls != 1 {
				t.Fatalf("redirect result error=%v, requests=%d", err, calls)
			}
		})
	}
}

func TestPublicWebBoundsResponsesAndRejectsCompression(t *testing.T) {
	for _, test := range []struct {
		name     string
		response *http.Response
		maximum  int64
	}{
		{name: "too large", response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("12345"))}, maximum: 4},
		{name: "compressed", response: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Encoding": []string{"gzip"}}, Body: io.NopCloser(strings.NewReader("data"))}, maximum: 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			web := NewPublicWeb()
			web.client.Transport = webRoundTrip(func(request *http.Request) (*http.Response, error) {
				response := *test.response
				response.Request = request
				return &response, nil
			})
			if _, _, _, err := web.FetchBounded(context.Background(), "https://example.com/data", test.maximum, nil); err == nil {
				t.Fatal("accepted an oversized or compressed response")
			}
		})
	}
}

func TestPublicWebRejectsExcessiveRedirects(t *testing.T) {
	previous := make([]*http.Request, 5)
	request, err := http.NewRequest(http.MethodGet, "https://example.com/next", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkPublicRedirect(request, previous); err == nil {
		t.Fatal("accepted excessive redirects")
	}
}

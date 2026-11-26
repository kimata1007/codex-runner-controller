package zerotrust

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// PublicWeb performs bounded, unauthenticated HTTPS fetches. It is not a generic
// forward proxy. DNS is resolved once per connection and the chosen public IP
// is pinned for the dial. Network policy must also enforce public-only egress.
type PublicWeb struct {
	client *http.Client
}

var forbiddenNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("168.63.129.16/32"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("2002::/16"),
}

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range forbiddenNetworks {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func validateWebURL(u *url.URL) error {
	if u == nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" || u.Opaque != "" || u.Fragment != "" {
		return errors.New("only absolute HTTPS URLs without userinfo or fragments are allowed")
	}
	if port := u.Port(); port != "" && port != "443" {
		return errors.New("only HTTPS port 443 is allowed")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if !strings.Contains(host, ".") || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".svc") || strings.HasSuffix(host, ".cluster.local") {
		return errors.New("non-public hostname")
	}
	if ip, err := netip.ParseAddr(host); err == nil && !publicIP(ip) {
		return errors.New("non-public address")
	}
	return nil
}

func NewPublicWeb() *PublicWeb {
	return newPublicWeb(net.DefaultResolver, &net.Dialer{Timeout: 5 * time.Second})
}

func newPublicWeb(resolver *net.Resolver, dialer *net.Dialer) *PublicWeb {
	transport := &http.Transport{
		// Never honor ambient HTTP_PROXY variables.
		Proxy:                  nil,
		DisableKeepAlives:      true,
		DisableCompression:     true,
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  10 * time.Second,
		MaxResponseHeaderBytes: 64 << 10,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil || port != "443" {
				return nil, errors.New("invalid web destination")
			}
			ips, err := resolver.LookupNetIP(ctx, "ip", host)
			if err != nil || !publicDNSAnswers(ips) {
				return nil, errors.New("web DNS resolution failed")
			}
			for _, ip := range ips {
				conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
			}
			return nil, errors.New("web connection failed")
		},
	}
	return &PublicWeb{client: &http.Client{
		Transport:     transport,
		Timeout:       20 * time.Second,
		CheckRedirect: checkPublicRedirect,
	}}
}

func publicDNSAnswers(ips []netip.Addr) bool {
	if len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return false
		}
	}
	return true
}

func checkPublicRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("too many redirects")
	}
	return validateWebURL(req.URL)
}

type WebResult struct {
	Status      int    `json:"status"`
	ContentType string `json:"contentType"`
	Body        string `json:"body"`
}

func (w *PublicWeb) Fetch(ctx context.Context, rawURL string) (WebResult, error) {
	status, media, body, err := w.FetchBounded(ctx, rawURL, 2<<20, nil)
	return WebResult{Status: status, ContentType: media, Body: string(body)}, err
}

// FetchBounded additionally restricts every redirect when allowHost is set.
func (w *PublicWeb) FetchBounded(ctx context.Context, rawURL string, maximum int64, allowHost func(string) bool) (int, string, []byte, error) {
	if maximum < 1 || maximum > 32<<20 {
		return 0, "", nil, errors.New("invalid download limit")
	}
	if len(rawURL) > 8192 {
		return 0, "", nil, errors.New("URL too long")
	}
	u, err := url.Parse(rawURL)
	if err != nil || validateWebURL(u) != nil || (allowHost != nil && !allowHost(u.Hostname())) {
		return 0, "", nil, errors.New("invalid public web URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, "", nil, err
	}
	req.Header.Set("User-Agent", "codex-runner-web-proxy/1")
	req.Header.Set("Accept-Encoding", "identity")
	httpClient := *w.client
	if allowHost != nil {
		check := httpClient.CheckRedirect
		httpClient.CheckRedirect = func(r *http.Request, previous []*http.Request) error {
			if !allowHost(r.URL.Hostname()) {
				return errors.New("download redirect is outside the approved registry")
			}
			return check(r, previous)
		}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, "", nil, errors.New("web fetch failed")
	}
	defer resp.Body.Close()
	if enc := resp.Header.Get("Content-Encoding"); enc != "" && enc != "identity" {
		return 0, "", nil, errors.New("compressed responses not supported")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maximum+1))
	if err != nil || int64(len(body)) > maximum {
		return 0, "", nil, errors.New("web response too large or unreadable")
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), body, nil
}

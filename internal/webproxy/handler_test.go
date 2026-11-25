package webproxy

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type searchRoundTrip func(*http.Request) (*http.Response, error)

func (f searchRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBraveSearchMakesOnlyScopedProviderRequest(t *testing.T) {
	s := NewBraveSearch(func() (string, error) { return "fixture-key", nil })
	calls := 0
	s.client.Transport = searchRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Scheme != "https" || r.URL.Host != "api.search.brave.com" || r.URL.Path != "/res/v1/web/search" ||
			r.URL.Query().Get("q") != "Go modules & tools" || r.Header.Get("X-Subscription-Token") != "fixture-key" {
			t.Fatal("unexpected Brave Search request")
		}
		return &http.Response{StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(`{"web":{"results":[{"title":"Module","url":"https://example.com","description":"Example"}]}}`))}, nil
	})
	results, err := s.Query(t.Context(), "Go modules & tools")
	if err != nil || len(results) != 1 || results[0].URL != "https://example.com" {
		t.Fatalf("unexpected search results %+v (%v)", results, err)
	}
	if _, err := s.Query(t.Context(), strings.Repeat("long ", 100)); !errors.Is(err, errSearchQuery) || calls != 1 {
		t.Fatalf("invalid query was forwarded: %v", err)
	}
}

func TestWebProxyRejectsUnboundCallerAndBoundsRate(t *testing.T) {
	h := New(nil, "codex.local", nil)
	for _, path := range []string{"/v1/search?q=test", "/v1/fetch?url=https://example.com", "/packages/go/test"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("X-Session-Identity", "spiffe://codex.local/session/test/pod/uid-test")
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s returned %d without a bound identity", path, response.Code)
		}
	}
	for range 4 {
		if !h.allow("session-1", 30) {
			t.Fatal("expected initial capacity")
		}
	}
	if h.allow("session-1", 30) || !h.allow("session-2", 30) {
		t.Fatal("rate limit is not session-scoped")
	}
}

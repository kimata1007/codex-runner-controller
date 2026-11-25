package webproxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/zerotrust"
	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/types"
)

type SessionAuthorizer interface {
	Session(context.Context, zerotrust.Identity) (zerotrust.SessionAuthorization, error)
}

type Handler struct {
	auth   SessionAuthorizer
	domain string
	web    *zerotrust.PublicWeb
	search *BraveSearch
	slots  chan struct{}
	large  chan struct{}
	mu     sync.Mutex
	rates  map[types.UID]budget
}

type budget struct {
	limit *rate.Limiter
	at    time.Time
}

func New(auth SessionAuthorizer, domain string, search *BraveSearch) *Handler {
	return &Handler{auth: auth, domain: domain, search: search, web: zerotrust.NewPublicWeb(),
		slots: make(chan struct{}, 24), large: make(chan struct{}, 2), rates: make(map[types.UID]budget)}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	id, ok := zerotrust.LocalIdentity(r.Context())
	if !ok {
		http.Error(w, "Web operation denied", http.StatusForbidden)
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		http.Error(w, "Web operation limit reached", http.StatusTooManyRequests)
		return
	}
	policy, err := h.auth.Session(r.Context(), id)
	target := zerotrust.Identity("spiffe://" + h.domain + "/proxy/web")
	if err != nil || policy.Web == nil || !policy.Policy.Allows(id, target, zerotrust.ProxyPort, time.Now()) {
		http.Error(w, "Web operation denied", http.StatusForbidden)
		return
	}
	web := policy.Web
	allowed := false
	operation := ""
	registry, packagePath, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/packages/"), "/")
	switch r.URL.Path {
	case "/v1/fetch":
		allowed, operation = web.Fetch, "web.fetch"
	case "/v1/search":
		allowed, operation = web.Search, "web.search"
	default:
		allowed, operation = strings.HasPrefix(r.URL.Path, "/packages/") && slices.Contains(web.Registries, registry), "web.package"
	}
	if !allowed {
		http.Error(w, "Web operation denied", http.StatusForbidden)
		return
	}
	if !h.allow(policy.Workload.SessionUID, web.RequestsPerMinute) {
		http.Error(w, "Web operation rate limit reached", http.StatusTooManyRequests)
		return
	}
	slog.InfoContext(r.Context(), "web operation", "operation", operation, "source_identity", id,
		"destination_identity", target, "decision", "allow", "policy_version", policy.Version)
	switch operation {
	case "web.fetch":
		result, err := h.web.Fetch(r.Context(), r.URL.Query().Get("url"))
		respond(w, result, err)
	case "web.search":
		if h.search == nil {
			http.Error(w, "Search provider unavailable", http.StatusServiceUnavailable)
			return
		}
		result, err := h.search.Query(r.Context(), r.URL.Query().Get("q"))
		respond(w, result, err)
	case "web.package":
		h.download(w, r, registry, packagePath)
	}
}

func (h *Handler) allow(uid types.UID, perMinute int64) bool {
	if uid == "" || perMinute <= 0 || perMinute > 600 {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	b, ok := h.rates[uid]
	if !ok {
		if len(h.rates) >= 2048 {
			for key, entry := range h.rates {
				if now.Sub(entry.at) >= 10*time.Minute {
					delete(h.rates, key)
				}
			}
		}
		if len(h.rates) >= 2048 {
			return false
		}
		b.limit = rate.NewLimiter(rate.Limit(perMinute)/60, 4)
	} else {
		b.limit.SetLimitAt(now, rate.Limit(perMinute)/60)
	}
	b.at = now
	h.rates[uid] = b
	return b.limit.AllowN(now, 1)
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request, registry, path string) {
	var host string
	switch registry {
	case "go":
		host = "proxy.golang.org"
	case "npm":
		host = "registry.npmjs.org"
	case "pypi":
		host = "pypi.org"
	default:
		http.NotFound(w, r)
		return
	}
	if path == "" || r.URL.RawQuery != "" || strings.Contains(path, "\\") {
		http.NotFound(w, r)
		return
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			http.NotFound(w, r)
			return
		}
	}
	select {
	case h.large <- struct{}{}:
		defer func() { <-h.large }()
	default:
		http.Error(w, "Package operation limit reached", http.StatusTooManyRequests)
		return
	}
	target := &url.URL{Scheme: "https", Host: host, Path: "/" + path}
	if registry == "pypi" && strings.HasPrefix(path, "files/") {
		target.Host, target.Path = "files.pythonhosted.org", "/"+strings.TrimPrefix(path, "files/")
	}
	status, media, data, err := h.web.FetchBounded(r.Context(), target.String(), 32<<20, func(candidate string) bool {
		return candidate == host || (registry == "go" && candidate == "storage.googleapis.com") ||
			(registry == "pypi" && candidate == "files.pythonhosted.org")
	})
	if err != nil {
		http.Error(w, "Package fetch failed", http.StatusBadGateway)
		return
	}
	if status != http.StatusOK {
		if status == http.StatusNotFound || status == http.StatusGone {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "Package fetch failed", http.StatusBadGateway)
		return
	}
	if registry == "npm" && strings.Contains(media, "json") {
		data = []byte(strings.ReplaceAll(string(data), "https://registry.npmjs.org/", "http://198.18.0.12:8080/packages/npm/"))
	}
	if registry == "pypi" && strings.Contains(media, "html") {
		data = []byte(strings.ReplaceAll(string(data), "https://files.pythonhosted.org/", "/packages/pypi/files/"))
	}
	w.Header().Set("Content-Type", media)
	_, _ = w.Write(data)
}

func respond(w http.ResponseWriter, result any, err error) {
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, errSearchQuery) {
			status = http.StatusBadRequest
		}
		http.Error(w, "Web operation failed", status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

var errSearchQuery = errors.New("invalid search query")

type BraveSearch struct {
	Credential func() (string, error)
	client     *http.Client
}

func NewBraveSearch(credential func() (string, error)) *BraveSearch {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &BraveSearch{Credential: credential, client: &http.Client{Transport: transport, Timeout: 12 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}

type SearchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

func (s *BraveSearch) Query(ctx context.Context, query string) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 600 || len(strings.Fields(query)) > 75 {
		return nil, errSearchQuery
	}
	token, err := s.Credential()
	token = strings.TrimSpace(token)
	if err != nil || token == "" || strings.ContainsAny(token, "\r\n \t") {
		return nil, errors.New("search provider unavailable")
	}
	endpoint := "https://api.search.brave.com/res/v1/web/search?" + url.Values{"q": {query}, "count": {"10"}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("X-Subscription-Token", token)
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, errors.New("search upstream unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("search provider rejected the request")
	}
	const limit = 1 << 20
	bytes, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	var result struct {
		Web struct {
			Results []SearchResult `json:"results"`
		} `json:"web"`
	}
	if err != nil || len(bytes) > limit || json.Unmarshal(bytes, &result) != nil {
		return nil, errors.New("invalid search response")
	}
	return result.Web.Results[:min(10, len(result.Web.Results))], nil
}

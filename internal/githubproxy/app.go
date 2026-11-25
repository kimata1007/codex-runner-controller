package githubproxy

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const apiVersion = "2026-03-10"

var errUpstream = errors.New("GitHub operation failed")

var (
	ownerPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
	repoPattern  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
)

type Repository struct{ Owner, Name string }

func (r Repository) valid() bool {
	return ownerPattern.MatchString(r.Owner) && repoPattern.MatchString(r.Name) && r.Name != "." && r.Name != ".."
}

func (r Repository) path() string { return r.Owner + "/" + r.Name }

type tokenScope int

const (
	readGit tokenScope = iota
	writeGit
	writePR
	readChecks
)

type installationKey struct {
	repository Repository
	scope      tokenScope
}

type tokenEntry struct {
	gate    chan struct{}
	value   string
	expires time.Time
}

type App struct {
	ID      string
	Key     func() ([]byte, error)
	client  *http.Client
	mu      sync.Mutex
	entries map[installationKey]*tokenEntry
}

func NewApp(id string, key func() ([]byte, error)) (*App, error) {
	if parsed, err := strconv.ParseInt(id, 10, 64); err != nil || parsed <= 0 || key == nil {
		return nil, errors.New("invalid GitHub App configuration")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.ResponseHeaderTimeout = 20 * time.Second
	transport.MaxConnsPerHost = 32
	return &App{ID: id, Key: key, entries: make(map[installationKey]*tokenEntry), client: &http.Client{
		Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (a *App) token(ctx context.Context, repo Repository, scope tokenScope) (string, error) {
	if !repo.valid() {
		return "", errUpstream
	}
	key := installationKey{repository: repo, scope: scope}
	a.mu.Lock()
	entry := a.entries[key]
	if entry == nil && len(a.entries) < 1024 {
		entry = &tokenEntry{gate: make(chan struct{}, 1)}
		a.entries[key] = entry
	}
	a.mu.Unlock()
	if entry == nil {
		return "", errUpstream
	}
	select {
	case entry.gate <- struct{}{}:
		defer func() { <-entry.gate }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if entry.value != "" && time.Until(entry.expires) > time.Minute {
		return entry.value, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	jwt, err := a.jwt(time.Now())
	if err != nil {
		return "", errUpstream
	}
	var installed struct{ ID int64 }
	if _, err := a.api(ctx, http.MethodGet, "/repos/"+repo.path()+"/installation", jwt, nil, &installed); err != nil || installed.ID <= 0 {
		return "", errUpstream
	}
	permissions := map[string]string{"contents": "read"}
	switch scope {
	case writeGit:
		permissions["contents"] = "write"
	case writePR:
		permissions["pull_requests"] = "write"
	case readChecks:
		permissions["pull_requests"] = "read"
		permissions["checks"] = "read"
	}
	payload := struct {
		Repositories []string          `json:"repositories"`
		Permissions  map[string]string `json:"permissions"`
	}{[]string{repo.Name}, permissions}
	var token struct {
		Value   string    `json:"token"`
		Expires time.Time `json:"expires_at"`
	}
	if _, err := a.api(ctx, http.MethodPost, "/app/installations/"+strconv.FormatInt(installed.ID, 10)+"/access_tokens", jwt, payload, &token); err != nil ||
		token.Value == "" || !time.Now().Before(token.Expires) {
		return "", errUpstream
	}
	entry.value, entry.expires = token.Value, token.Expires
	return entry.value, nil
}

func (a *App) invalidate(repo Repository, scope tokenScope, token string) {
	key := installationKey{repository: repo, scope: scope}
	a.mu.Lock()
	entry := a.entries[key]
	a.mu.Unlock()
	if entry != nil {
		// A concurrent renewal may already have replaced this token.
		select {
		case entry.gate <- struct{}{}:
			if entry.value == token {
				entry.value = ""
			}
			<-entry.gate
		default:
		}
	}
}

func (a *App) jwt(now time.Time) (string, error) {
	raw, err := a.Key()
	if err != nil || len(raw) > 16<<10 {
		return "", errUpstream
	}
	defer clear(raw)
	block, _ := pem.Decode(raw)
	if block == nil {
		return "", errUpstream
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		value, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		var ok bool
		key, ok = value.(*rsa.PrivateKey)
		if err != nil || !ok {
			return "", errUpstream
		}
	}
	if key.N.BitLen() < 2048 {
		return "", errUpstream
	}
	claims, _ := json.Marshal(map[string]any{"iat": now.Unix() - 60, "exp": now.Add(8 * time.Minute).Unix(), "iss": a.ID})
	encoding := base64.RawURLEncoding
	input := encoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + encoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(input))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", errUpstream
	}
	return input + "." + encoding.EncodeToString(signature), nil
}

func (a *App) api(ctx context.Context, method, path, token string, payload, target any) (int, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.github.com"+path, body)
	if err != nil || strings.ContainsAny(token, "\r\n \t") {
		return 0, errUpstream
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := a.client.Do(req)
	if err != nil {
		return 0, errUpstream
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return res.StatusCode, errUpstream
	}
	const limit = 2 << 20
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil || len(data) > limit || json.Unmarshal(data, target) != nil {
		return res.StatusCode, errUpstream
	}
	return res.StatusCode, nil
}

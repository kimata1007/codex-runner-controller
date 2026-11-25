package githubproxy

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPushChecksExactReferencesAndPreservesThePack(t *testing.T) {
	const prefix = "codex/test-session/"
	pack := "PACK bytes follow the header"
	oid := strings.Repeat("a", 40)
	packet := func(text string) string { return fmt.Sprintf("%04x%s", len(text)+4, text) }
	header := packet(zeroSHA1 + " " + oid + " refs/heads/" + prefix + "feature\x00 report-status-v2 side-band-64k object-format=sha1\n")
	full := header + "0000" + pack
	body, refs, err := readPush(strings.NewReader(full), prefix, "main")
	if err != nil || len(refs) != 1 || refs[0] != prefix+"feature" {
		t.Fatalf("unexpected push result %#v: %v", refs, err)
	}
	retained, err := io.ReadAll(body)
	if err != nil || string(retained) != full {
		t.Fatal("Git request changed after validation")
	}
	for _, denied := range []string{
		packet(zeroSHA1 + " " + oid + " refs/heads/main"),
		packet(zeroSHA1 + " " + oid + " refs/heads/codex/other/feature"),
		packet(zeroSHA1 + " " + oid + " refs/tags/release"),
		packet(oid + " " + zeroSHA1 + " refs/heads/" + prefix + "feature"),
		packet(zeroSHA1 + " " + oid + " refs/heads/" + prefix + "feature\x00 push-options"),
		header + packet(zeroSHA1+" "+oid+" refs/heads/main"),
	} {
		if _, _, err := readPush(strings.NewReader(denied+"0000"+pack), prefix, "main"); err == nil {
			t.Fatal("an unapproved push was accepted")
		}
	}
}

type appRoundTrip func(*http.Request) (*http.Response, error)

func (f appRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAppTokensUseSeparateRepositoryPermissions(t *testing.T) {
	signingKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	app, err := NewApp("42", func() ([]byte, error) {
		return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(signingKey)}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	app.client.Transport = appRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "api.github.com" || r.Header.Get("X-GitHub-Api-Version") != apiVersion ||
			strings.Count(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".") != 2 {
			t.Fatal("GitHub App authentication metadata did not match")
		}
		body := `{"id":99}`
		switch r.URL.Path {
		case "/repos/example/project/installation":
		case "/app/installations/99/access_tokens":
			var request struct {
				Repositories []string
				Permissions  map[string]string
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil ||
				len(request.Repositories) != 1 || request.Repositories[0] != "project" ||
				len(request.Permissions) != 1 {
				t.Fatal("token permissions were not repository-scoped")
			}
			body = fmt.Sprintf(`{"token":"fixture-%s","expires_at":%q}`, request.Permissions["contents"], time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
		default:
			t.Fatal("unexpected GitHub call")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	repo := Repository{"example", "project"}
	for _, mode := range []struct {
		scope tokenScope
		want  string
	}{{readGit, "fixture-read"}, {readGit, "fixture-read"}, {writeGit, "fixture-write"}} {
		token, err := app.token(t.Context(), repo, mode.scope)
		if err != nil || token != mode.want {
			t.Fatalf("scope %d returned %s (%v)", mode.scope, token, err)
		}
	}
	if calls != 4 {
		t.Fatalf("received %d GitHub calls, want cached read token", calls)
	}
}

func TestStandardGitCloneFetchAndScopedPush(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git is unavailable")
	}
	directory := t.TempDir()
	run := func(work string, args ...string) (string, error) {
		command := exec.CommandContext(t.Context(), git, args...)
		command.Dir = work
		command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0")
		output, err := command.CombinedOutput()
		return string(output), err
	}
	must := func(work string, args ...string) string {
		t.Helper()
		output, err := run(work, args...)
		if err != nil {
			t.Fatalf("git %q: %v\n%s", args, err, output)
		}
		return output
	}
	root := filepath.Join(directory, "repositories")
	bare := filepath.Join(root, "owner", "project.git")
	if err := os.MkdirAll(filepath.Dir(bare), 0700); err != nil {
		t.Fatal(err)
	}
	must(directory, "init", "--bare", bare)
	must(bare, "config", "http.receivepack", "true")
	must(bare, "config", "receive.denyNonFastForwards", "true")
	backend := filepath.Join(strings.TrimSpace(must(directory, "--exec-path")), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skip("git-http-backend is unavailable")
	}
	origin := httptest.NewServer(&cgi.Handler{Path: backend, Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}})
	defer origin.Close()
	originURL, err := url.Parse(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	repo := Repository{Owner: "owner", Name: "project"}
	app := &App{entries: make(map[installationKey]*tokenEntry), client: &http.Client{}}
	for _, scope := range []tokenScope{readGit, writeGit} {
		app.entries[installationKey{repo, scope}] = &tokenEntry{gate: make(chan struct{}, 1), value: "fixture-token", expires: time.Now().Add(time.Hour)}
	}
	app.client.Transport = appRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.github.com" && strings.HasPrefix(r.URL.Path, "/repos/owner/project/rules/branches/codex/test-session/") {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`[{"type":"non_fast_forward"},{"type":"deletion"}]`))}, nil
		}
		if r.URL.Host != "github.com" {
			return nil, errUpstream
		}
		forwarded := r.Clone(r.Context())
		forwarded.URL.Host, forwarded.URL.Scheme = originURL.Host, originURL.Scheme
		return http.DefaultTransport.RoundTrip(forwarded)
	})
	h := &Handler{app: app}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		suffix, ok := strings.CutPrefix(r.URL.Path, "/owner/project.git")
		if !ok {
			http.NotFound(w, r)
			return
		}
		h.git(w, r, repo, grant{prefix: "codex/test-session/", base: "main", push: true}, suffix)
	}))
	defer proxy.Close()
	remote := proxy.URL + "/owner/project.git"
	checkout := filepath.Join(directory, "checkout")
	must(directory, "clone", remote, checkout)
	must(checkout, "switch", "-c", "codex/test-session/feature")
	for range 2 {
		must(checkout, "-c", "user.name=fixture", "-c", "user.email=fixture@example.com", "commit", "--allow-empty", "-m", "fixture")
		must(checkout, "push", "origin", "HEAD:refs/heads/codex/test-session/feature")
	}
	if output, err := run(checkout, "push", "origin", "HEAD:refs/heads/main"); err == nil {
		t.Fatalf("protected branch push succeeded: %s", output)
	}
	other := filepath.Join(directory, "other")
	must(directory, "clone", "--branch", "codex/test-session/feature", remote, other)
	must(other, "fetch", "origin")
	if got := must(other, "rev-parse", "HEAD"); got != must(checkout, "rev-parse", "HEAD") {
		t.Fatal("cloned revision does not match the approved push")
	}
}

package githubproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/zerotrust"
)

type fixedSessionAuthorization struct {
	value zerotrust.SessionAuthorization
}

func (a fixedSessionAuthorization) Session(context.Context, zerotrust.Identity) (zerotrust.SessionAuthorization, error) {
	return a.value, nil
}

type handlerRoundTrip func(*http.Request) (*http.Response, error)

func (f handlerRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPullRequestOperationsAreSessionAndBranchScoped(t *testing.T) {
	const (
		identity = zerotrust.Identity("spiffe://codex.local/session/test-session/pod/test-pod")
		proxyID  = zerotrust.Identity("spiffe://codex.local/proxy/github")
		branch   = "codex/session-uid/fix"
		sha      = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	repo := Repository{Owner: "example", Name: "project"}
	allowedPR := func(ref string) string {
		return fmt.Sprintf(`{"number":12,"html_url":"https://github.com/example/project/pull/12","state":"open","head":{"ref":%q,"sha":%q,"repo":{"full_name":"example/project"}},"base":{"ref":"main"}}`, ref, sha)
	}
	for _, test := range []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantCalls  int
	}{
		{name: "create draft", method: http.MethodPost, path: "/v1/pulls", body: `{"head":"` + branch + `","title":"Fix","body":"Details"}`, wantStatus: http.StatusCreated, wantCalls: 1},
		{name: "update", method: http.MethodPatch, path: "/v1/pulls/12", body: `{"title":"Updated"}`, wantStatus: http.StatusOK, wantCalls: 2},
		{name: "status", method: http.MethodGet, path: "/v1/pulls/12", wantStatus: http.StatusOK, wantCalls: 1},
		{name: "checks", method: http.MethodGet, path: "/v1/pulls/12/checks", wantStatus: http.StatusOK, wantCalls: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			app := &App{entries: make(map[installationKey]*tokenEntry), client: &http.Client{}}
			app.entries[installationKey{repository: repo, scope: writePR}] = &tokenEntry{gate: make(chan struct{}, 1), value: "fixture-pr-token", expires: time.Now().Add(time.Hour)}
			app.entries[installationKey{repository: repo, scope: readChecks}] = &tokenEntry{gate: make(chan struct{}, 1), value: "fixture-check-token", expires: time.Now().Add(time.Hour)}
			app.client.Transport = handlerRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "api.github.com" || r.Header.Get("Authorization") == "" {
					t.Fatalf("unexpected GitHub request: %s %s", r.Method, r.URL)
				}
				var status int
				var response string
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/repos/example/project/pulls":
					var payload map[string]any
					if json.NewDecoder(r.Body).Decode(&payload) != nil || payload["head"] != branch || payload["base"] != "main" || payload["draft"] != true || payload["maintainer_can_modify"] != false {
						t.Fatalf("unsafe pull request create payload: %#v", payload)
					}
					status, response = http.StatusCreated, allowedPR(branch)
				case r.Method == http.MethodGet && r.URL.Path == "/repos/example/project/pulls/12":
					status, response = http.StatusOK, allowedPR(branch)
				case r.Method == http.MethodPatch && r.URL.Path == "/repos/example/project/pulls/12":
					var payload map[string]string
					if json.NewDecoder(r.Body).Decode(&payload) != nil || payload["title"] != "Updated" {
						t.Fatalf("unexpected pull request update payload: %#v", payload)
					}
					status, response = http.StatusOK, allowedPR(branch)
				case r.Method == http.MethodGet && r.URL.Path == "/repos/example/project/commits/"+sha+"/check-runs":
					if r.URL.Query().Get("per_page") != "50" {
						t.Fatalf("checks were not bounded: %s", r.URL.RawQuery)
					}
					status, response = http.StatusOK, `{"check_runs":[{"name":"CI","status":"completed","conclusion":"success"}]}`
				default:
					t.Fatalf("unexpected upstream GitHub request: %s %s", r.Method, r.URL)
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
			})
			policy, err := zerotrust.NewPolicy("codex.local", time.Now().Add(time.Minute), []zerotrust.Grant{{Source: identity, Destination: proxyID, Port: zerotrust.ProxyPort}})
			if err != nil {
				t.Fatal(err)
			}
			authorizer := fixedSessionAuthorization{value: zerotrust.SessionAuthorization{
				Workload: v1alpha1.SessionWorkload{SessionUID: "session-uid"}, Version: 4, Policy: policy,
				GitHub: &v1alpha1.GitHubPolicy{Owner: repo.Owner, Repository: repo.Name, BaseBranch: "main", PullRequests: true},
			}}
			handler := New(app, authorizer, "codex.local")
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			response := httptest.NewRecorder()
			handler.serveAuthenticated(response, request, identity)
			if response.Code != test.wantStatus || calls != test.wantCalls {
				t.Fatalf("response = %d after %d upstream calls, want %d after %d", response.Code, calls, test.wantStatus, test.wantCalls)
			}
			if strings.Contains(response.Body.String(), "fixture-pr-token") || strings.Contains(response.Body.String(), "fixture-check-token") {
				t.Fatal("GitHub installation token escaped in proxy response")
			}
		})
	}
}

func TestPullRequestProxyRejectsOutOfScopeOperations(t *testing.T) {
	const identity = zerotrust.Identity("spiffe://codex.local/session/test-session/pod/test-pod")
	proxyID := zerotrust.Identity("spiffe://codex.local/proxy/github")
	policy, err := zerotrust.NewPolicy("codex.local", time.Now().Add(time.Minute), []zerotrust.Grant{{Source: identity, Destination: proxyID, Port: zerotrust.ProxyPort}})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	app := &App{entries: make(map[installationKey]*tokenEntry), client: &http.Client{Transport: handlerRoundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errUpstream
	})}}
	for _, test := range []struct {
		name   string
		policy *v1alpha1.GitHubPolicy
		method string
		path   string
		body   string
	}{
		{name: "other session branch", policy: &v1alpha1.GitHubPolicy{Owner: "example", Repository: "project", BaseBranch: "main", PullRequests: true}, method: http.MethodPost, path: "/v1/pulls", body: `{"head":"codex/other-session/feature","title":"No"}`},
		{name: "disabled PR grant", policy: &v1alpha1.GitHubPolicy{Owner: "example", Repository: "project", BaseBranch: "main"}, method: http.MethodPost, path: "/v1/pulls", body: `{"head":"codex/session-uid/feature","title":"No"}`},
		{name: "merge endpoint", policy: &v1alpha1.GitHubPolicy{Owner: "example", Repository: "project", BaseBranch: "main", PullRequests: true}, method: http.MethodPut, path: "/v1/pulls/12/merge"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := New(app, fixedSessionAuthorization{value: zerotrust.SessionAuthorization{
				Workload: v1alpha1.SessionWorkload{SessionUID: "session-uid"}, Version: 1, Policy: policy, GitHub: test.policy,
			}}, "codex.local")
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			response := httptest.NewRecorder()
			handler.serveAuthenticated(response, request, identity)
			if response.Code != http.StatusForbidden && response.Code != http.StatusNotFound {
				t.Fatalf("out-of-scope operation returned HTTP %d", response.Code)
			}
			if calls != 0 {
				t.Fatal("denied operation reached GitHub")
			}
		})
	}
}

package githubproxy

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/zerotrust"
)

type SessionAuthorizer interface {
	Session(context.Context, zerotrust.Identity) (zerotrust.SessionAuthorization, error)
}

type Handler struct {
	app    *App
	auth   SessionAuthorizer
	domain string
	slots  chan struct{}
}

type grant struct {
	prefix, base string
	push, pr     bool
}

func New(app *App, auth SessionAuthorizer, domain string) *Handler {
	return &Handler{app: app, auth: auth, domain: domain, slots: make(chan struct{}, 32)}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	identity, ok := zerotrust.LocalIdentity(r.Context())
	if !ok {
		http.Error(w, "GitHub operation denied", http.StatusForbidden)
		return
	}
	h.serveAuthenticated(w, r, identity)
}

// serveAuthenticated applies the per-session policy and operation checks after
// ServeHTTP has obtained the transport-authenticated local workload identity.
func (h *Handler) serveAuthenticated(w http.ResponseWriter, r *http.Request, identity zerotrust.Identity) {
	w.Header().Set("Cache-Control", "no-store")
	ctx, stop := context.WithTimeout(r.Context(), 5*time.Minute)
	defer stop()
	r = r.WithContext(ctx)
	policy, err := h.auth.Session(ctx, identity)
	target := zerotrust.Identity("spiffe://" + h.domain + "/proxy/github")
	if err != nil || policy.GitHub == nil || !policy.Policy.Allows(identity, target, zerotrust.ProxyPort, time.Now()) {
		http.Error(w, "GitHub operation denied", http.StatusForbidden)
		return
	}
	p := policy.GitHub
	repo := Repository{Owner: p.Owner, Name: p.Repository}
	session := grant{prefix: "codex/" + string(policy.Workload.SessionUID) + "/", base: p.BaseBranch, push: p.Push, pr: p.PullRequests}
	if !repo.valid() || !branchPattern.MatchString(session.base) || len(session.base) > 128 ||
		!branchPattern.MatchString(strings.TrimSuffix(session.prefix, "/")) || policy.Workload.SessionUID == "" {
		http.Error(w, "GitHub operation denied", http.StatusForbidden)
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		http.Error(w, "GitHub operation limit reached", http.StatusTooManyRequests)
		return
	}
	operation := "github.pull_request"
	gitSuffix, isGit := strings.CutPrefix(r.URL.Path, "/"+repo.path()+".git")
	if isGit && (gitSuffix == "/info/refs" || gitSuffix == "/git-upload-pack" || gitSuffix == "/git-receive-pack") {
		operation = "github.git"
	}
	slog.InfoContext(ctx, "github operation", "operation", operation, "source_identity", identity,
		"destination_identity", target, "repository", repo.path(), "policy_version", policy.Version)
	if operation == "github.git" {
		h.git(w, r, repo, session, gitSuffix)
		return
	}
	if !session.pr || r.URL.RawQuery != "" {
		http.Error(w, "GitHub operation denied", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/v1/pulls" {
		h.create(w, r, repo, session)
		return
	}
	remaining, ok := strings.CutPrefix(r.URL.Path, "/v1/pulls/")
	rawNumber, action, _ := strings.Cut(remaining, "/")
	number, err := strconv.ParseUint(rawNumber, 10, 32)
	if !ok || err != nil || number == 0 || (action != "" && action != "checks") ||
		(r.Method != http.MethodGet && !(r.Method == http.MethodPatch && action == "")) {
		http.NotFound(w, r)
		return
	}
	h.withPR(w, r, repo, session, strconv.FormatUint(number, 10), action)
}

type pullRequest struct {
	Number  uint64 `json:"number"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
	Head    struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (p pullRequest) allowed(repo Repository, session grant) bool {
	return p.Number > 0 && p.Head.Repo.FullName == repo.path() && p.Base.Ref == session.base &&
		allowedBranch(p.Head.Ref, session.prefix, session.base) && shaPattern.MatchString(p.Head.SHA) &&
		p.HTMLURL == "https://github.com/"+repo.path()+"/pull/"+strconv.FormatUint(p.Number, 10)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, repo Repository, session grant) {
	var request struct {
		Head  string `json:"head"`
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if !readJSON(w, r, &request) || !allowedBranch(request.Head, session.prefix, session.base) ||
		strings.TrimSpace(request.Title) == "" || len(request.Title) > 256 || len(request.Body) > 65536 {
		http.Error(w, "Pull request denied", http.StatusForbidden)
		return
	}
	token, err := h.app.token(r.Context(), repo, writePR)
	if err != nil {
		http.Error(w, "GitHub unavailable", http.StatusBadGateway)
		return
	}
	var result pullRequest
	payload := map[string]any{"head": request.Head, "base": session.base, "title": request.Title, "body": request.Body,
		"draft": true, "maintainer_can_modify": false}
	if !h.api(w, r, repo, writePR, http.MethodPost, "/repos/"+repo.path()+"/pulls", token, payload, &result) {
		return
	}
	if !result.allowed(repo, session) {
		http.Error(w, "Unexpected GitHub response", http.StatusBadGateway)
		return
	}
	reply(w, http.StatusCreated, result)
}

func (h *Handler) withPR(w http.ResponseWriter, r *http.Request, repo Repository, session grant, number, action string) {
	scope := readChecks
	if r.Method == http.MethodPatch {
		scope = writePR
	}
	token, err := h.app.token(r.Context(), repo, scope)
	if err != nil {
		http.Error(w, "GitHub unavailable", http.StatusBadGateway)
		return
	}
	root := "/repos/" + repo.path()
	path := root + "/pulls/" + number
	var result pullRequest
	if !h.api(w, r, repo, scope, http.MethodGet, path, token, nil, &result) {
		return
	}
	if !result.allowed(repo, session) {
		http.Error(w, "Pull request denied", http.StatusForbidden)
		return
	}
	if action == "checks" {
		var checks struct {
			Runs []struct {
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
			} `json:"check_runs"`
		}
		if h.api(w, r, repo, scope, http.MethodGet, root+"/commits/"+result.Head.SHA+"/check-runs?per_page=50", token, nil, &checks) {
			reply(w, http.StatusOK, checks)
		}
		return
	}
	if r.Method == http.MethodPatch {
		var update struct {
			Title *string `json:"title,omitempty"`
			Body  *string `json:"body,omitempty"`
		}
		if !readJSON(w, r, &update) || (update.Title == nil && update.Body == nil) ||
			(update.Title != nil && (strings.TrimSpace(*update.Title) == "" || len(*update.Title) > 256)) ||
			(update.Body != nil && len(*update.Body) > 65536) {
			http.Error(w, "Pull request update denied", http.StatusForbidden)
			return
		}
		if !h.api(w, r, repo, scope, http.MethodPatch, path, token, update, &result) {
			return
		}
		if !result.allowed(repo, session) {
			http.Error(w, "Unexpected GitHub response", http.StatusBadGateway)
			return
		}
	}
	reply(w, http.StatusOK, result)
}

func (h *Handler) api(w http.ResponseWriter, r *http.Request, repo Repository, scope tokenScope, method, path, token string, payload, target any) bool {
	code, err := h.app.api(r.Context(), method, path, token, payload, target)
	if code == http.StatusUnauthorized {
		h.app.invalidate(repo, scope, token)
	}
	if err != nil {
		http.Error(w, "GitHub rejected the operation", http.StatusBadGateway)
	}
	return err == nil
}

func readJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 80<<10))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target) == nil && decoder.Decode(new(json.RawMessage)) == io.EOF
}

func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

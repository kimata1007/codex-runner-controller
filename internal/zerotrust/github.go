package zerotrust

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var repositoryPart = regexp.MustCompile("^[A-Za-z0-9_.-]+$")
var branchPart = regexp.MustCompile("^[a-zA-Z0-9_-]+(/[a-zA-Z0-9_-]+)*$")

// PullRequestGrant must come from trusted policy bound to the authenticated
// session, not the request body. Repository and prefix are case-sensitive.
type PullRequestGrant struct {
	Owner, Repository, BranchPrefix, Base string
}

type PullRequest struct {
	Head, Title, Body string
}

// GitHubTokenSource is implemented by a trusted GitHub App installation token
// provider. Tokens must be restricted to the target repository.
type GitHubTokenSource interface {
	Token(context.Context, string, string) (string, error)
}

func CreatePullRequest(ctx context.Context, tokens GitHubTokenSource, grant PullRequestGrant, pr PullRequest) (string, error) {
	if tokens == nil || !repositoryPart.MatchString(grant.Owner) || !repositoryPart.MatchString(grant.Repository) ||
		grant.Owner == "." || grant.Owner == ".." || grant.Repository == "." || grant.Repository == ".." ||
		!strings.HasSuffix(grant.BranchPrefix, "/") || !strings.HasPrefix(grant.BranchPrefix, "codex/") ||
		!branchPart.MatchString(strings.TrimSuffix(grant.BranchPrefix, "/")) ||
		!branchPart.MatchString(pr.Head) || !strings.HasPrefix(pr.Head, grant.BranchPrefix) ||
		!branchPart.MatchString(grant.Base) || pr.Head == grant.Base ||
		strings.TrimSpace(pr.Title) == "" || len(pr.Title) > 256 || len(pr.Body) > 65536 {
		return "", errors.New("pull request denied")
	}
	token, err := tokens.Token(ctx, grant.Owner, grant.Repository)
	if err != nil || token == "" {
		return "", errors.New("GitHub credential unavailable")
	}
	body, err := json.Marshal(map[string]string{
		"head": pr.Head, "base": grant.Base, "title": pr.Title, "body": pr.Body,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/repos/"+grant.Owner+"/"+grant.Repository+"/pulls", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
			TLSHandshakeTimeout: 5 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("GitHub request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", errors.New("GitHub rejected pull request")
	}
	var result struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return "", errors.New("invalid GitHub response")
	}
	if !strings.HasPrefix(result.HTMLURL, "https://github.com/"+grant.Owner+"/"+grant.Repository+"/pull/") {
		return "", errors.New("unexpected GitHub response URL")
	}
	return result.HTMLURL, nil
}

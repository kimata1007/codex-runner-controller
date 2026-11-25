package githubproxy

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	branchPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+(/[A-Za-z0-9_-]+)*$`)
	shaPattern    = regexp.MustCompile(`^[a-f0-9]{40}$`)
)

const zeroSHA1 = "0000000000000000000000000000000000000000"

func allowedBranch(branch, prefix, base string) bool {
	return len(branch) <= 200 && len(prefix) > 7 && branchPattern.MatchString(branch) &&
		strings.HasPrefix(branch, prefix) && branch != base
}

func readPush(input io.Reader, prefix, base string) (io.Reader, []string, error) {
	reader := bufio.NewReader(input)
	var retained bytes.Buffer
	var branches []string
	seen := make(map[string]bool)
	for {
		var header [4]byte
		if _, err := io.ReadFull(reader, header[:]); err != nil {
			return nil, nil, errors.New("invalid push header")
		}
		size, err := strconv.ParseUint(string(header[:]), 16, 16)
		if err != nil || size > 65520 || (size > 0 && size <= 4) || retained.Len()+max(4, int(size)) > 128<<10 {
			return nil, nil, errors.New("invalid push packet size")
		}
		retained.Write(header[:])
		if size == 0 {
			if len(branches) == 0 {
				return nil, nil, errors.New("empty push")
			}
			return io.MultiReader(bytes.NewReader(retained.Bytes()), reader), branches, nil
		}
		packet := make([]byte, int(size)-4)
		if _, err := io.ReadFull(reader, packet); err != nil {
			return nil, nil, errors.New("incomplete push packet")
		}
		retained.Write(packet)
		command, caps, hasCaps := strings.Cut(strings.TrimSuffix(string(packet), "\n"), "\x00")
		if (len(branches) > 0 && hasCaps) || strings.ContainsAny(command, "\n\r\t") {
			return nil, nil, errors.New("invalid push command")
		}
		for _, capability := range strings.Fields(caps) {
			switch {
			case capability == "report-status", capability == "report-status-v2", capability == "side-band-64k",
				capability == "ofs-delta", capability == "atomic", capability == "quiet",
				capability == "object-format=sha1", strings.HasPrefix(capability, "agent="):
			default:
				return nil, nil, errors.New("unsupported push capability")
			}
		}
		fields := strings.Split(command, " ")
		if len(fields) != 3 || !shaPattern.MatchString(fields[0]) || !shaPattern.MatchString(fields[1]) || fields[1] == zeroSHA1 {
			return nil, nil, errors.New("invalid push ref update")
		}
		branch, ok := strings.CutPrefix(fields[2], "refs/heads/")
		if !ok || !allowedBranch(branch, prefix, base) || len(branches) >= 16 || seen[branch] {
			return nil, nil, errors.New("push branch denied")
		}
		seen[branch] = true
		branches = append(branches, branch)
	}
}

func (a *App) requireBranchRules(ctx context.Context, repo Repository, token, branch string) error {
	var rules []struct{ Type string }
	if _, err := a.api(ctx, http.MethodGet, "/repos/"+repo.path()+"/rules/branches/"+url.PathEscape(branch), token, nil, &rules); err != nil {
		return err
	}
	var nonFastForward, deletion bool
	for _, rule := range rules {
		nonFastForward = nonFastForward || rule.Type == "non_fast_forward"
		deletion = deletion || rule.Type == "deletion"
	}
	if !nonFastForward || !deletion {
		return errors.New("push requires applicable branch rules")
	}
	return nil
}

func (h *Handler) git(w http.ResponseWriter, r *http.Request, repo Repository, session grant, suffix string) {
	service := ""
	advertise := r.Method == http.MethodGet && suffix == "/info/refs"
	if advertise {
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil || len(query) != 1 || len(query["service"]) != 1 {
			http.NotFound(w, r)
			return
		}
		service = query.Get("service")
	} else if r.Method == http.MethodPost && r.URL.RawQuery == "" {
		service = strings.TrimPrefix(suffix, "/")
		if r.Header.Get("Content-Type") != "application/x-"+service+"-request" {
			http.Error(w, "Git operation denied", http.StatusForbidden)
			return
		}
	} else {
		http.NotFound(w, r)
		return
	}
	scope := readGit
	if service == "git-receive-pack" && session.push {
		scope = writeGit
	} else if service != "git-upload-pack" {
		http.Error(w, "Git operation denied", http.StatusForbidden)
		return
	}
	token, err := h.app.token(r.Context(), repo, scope)
	if err != nil {
		http.Error(w, "GitHub unavailable", http.StatusBadGateway)
		return
	}
	var body io.Reader
	if !advertise {
		body = http.MaxBytesReader(w, r.Body, 512<<20)
	}
	if scope == writeGit && !advertise {
		if r.Header.Get("Content-Encoding") != "" {
			http.Error(w, "Git push encoding denied", http.StatusForbidden)
			return
		}
		var branches []string
		body, branches, err = readPush(body, session.prefix, session.base)
		if err != nil {
			http.Error(w, "Git push denied", http.StatusForbidden)
			return
		}
		for _, branch := range branches {
			if err := h.app.requireBranchRules(r.Context(), repo, token, branch); err != nil {
				http.Error(w, "Git branch rules unavailable", http.StatusForbidden)
				return
			}
		}
	}
	endpoint := "https://github.com/" + repo.path() + ".git" + suffix
	if advertise {
		endpoint += "?service=" + service
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, endpoint, body)
	if err != nil {
		http.Error(w, "GitHub unavailable", http.StatusBadGateway)
		return
	}
	req.SetBasicAuth("x-access-token", token)
	if !advertise {
		req.Header.Set("Content-Type", "application/x-"+service+"-request")
		if r.ContentLength >= 0 {
			req.ContentLength = r.ContentLength
		}
	}
	if scope == readGit && r.Header.Get("Git-Protocol") == "version=2" {
		req.Header.Set("Git-Protocol", "version=2")
	}
	if scope == readGit && r.Header.Get("Content-Encoding") == "gzip" {
		req.Header.Set("Content-Encoding", "gzip")
	}
	upstream, err := h.app.client.Do(req)
	if err != nil {
		http.Error(w, "GitHub unavailable", http.StatusBadGateway)
		return
	}
	defer upstream.Body.Close()
	if upstream.StatusCode == http.StatusUnauthorized {
		h.app.invalidate(repo, scope, token)
	}
	content := "application/x-" + service + "-result"
	if advertise {
		content = "application/x-" + service + "-advertisement"
	}
	if upstream.StatusCode != http.StatusOK || upstream.Header.Get("Content-Type") != content {
		http.Error(w, "GitHub rejected the Git operation", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", content)
	_, _ = io.Copy(w, io.LimitReader(upstream.Body, 1<<30))
}

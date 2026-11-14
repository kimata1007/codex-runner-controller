package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// BearerToken protects an internal HTTP service. An empty expected token is
// an explicit development-mode bypass; production manifests must provide it
// through a Secret-backed environment variable.
func BearerToken(next http.Handler, expected string) http.Handler {
	return bearerToken(next, expected, func(*http.Request) bool { return true })
}

// BearerTokenForPathPrefix protects only requests whose URL path starts with
// prefix. This lets a trusted network boundary protect user-facing routes
// while internal service routes continue to require a shared token.
func BearerTokenForPathPrefix(next http.Handler, expected, prefix string) http.Handler {
	return bearerToken(next, expected, func(request *http.Request) bool {
		return strings.HasPrefix(request.URL.Path, prefix)
	})
}

func bearerToken(next http.Handler, expected string, shouldProtect func(*http.Request) bool) http.Handler {
	if strings.TrimSpace(expected) == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || !shouldProtect(r) {
			next.ServeHTTP(w, r)
			return
		}
		provided := strings.TrimSpace(r.Header.Get("Authorization"))
		want := "Bearer " + expected
		if len(provided) != len(want) || subtle.ConstantTimeCompare([]byte(provided), []byte(want)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="codex-runner"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

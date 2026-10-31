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
	if strings.TrimSpace(expected) == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
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

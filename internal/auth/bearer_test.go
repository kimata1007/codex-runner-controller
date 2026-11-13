package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBearerTokenProtectsRoutesAndLeavesHealthPublic(t *testing.T) {
	handler := BearerToken(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), "secret")

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/conversations", nil))
	if unauthorized.Code != http.StatusUnauthorized || unauthorized.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("unauthorized status=%d headers=%v", unauthorized.Code, unauthorized.Header())
	}

	authorizedRequest := httptest.NewRequest(http.MethodGet, "/v1/conversations", nil)
	authorizedRequest.Header.Set("Authorization", "Bearer secret")
	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusNoContent {
		t.Fatalf("authorized status=%d", authorized.Code)
	}

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusNoContent {
		t.Fatalf("health status=%d", health.Code)
	}
}

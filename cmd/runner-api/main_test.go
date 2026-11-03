package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRepositoryFromEnvironmentDefaultsToMemory(t *testing.T) {
	t.Setenv("LIBSQL_URL", "")

	repository, err := repositoryFromEnvironment(context.Background())
	if err != nil {
		t.Fatalf("repositoryFromEnvironment: %v", err)
	}
	if _, err := repository.CreateConversation(context.Background()); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
}

func TestRepositoryFromEnvironmentMigratesLibSQL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/pipeline" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing libSQL authorization")
		}
		var request struct {
			Requests []struct {
				Type string `json:"type"`
				SQL  string `json:"sql"`
			} `json:"requests"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if len(request.Requests) != 1 || request.Requests[0].Type != "sequence" || request.Requests[0].SQL == "" {
			t.Errorf("unexpected schema request: %+v", request.Requests)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"type":"ok","response":{"type":"sequence"}}]}`))
	}))
	defer server.Close()

	t.Setenv("LIBSQL_URL", server.URL)
	t.Setenv("LIBSQL_AUTH_TOKEN", "test-token")
	repository, err := repositoryFromEnvironment(context.Background())
	if err != nil {
		t.Fatalf("repositoryFromEnvironment: %v", err)
	}
	if repository == nil {
		t.Fatal("repository is nil")
	}
}

func TestRepositoryFromEnvironmentReturnsMigrationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	t.Setenv("LIBSQL_URL", server.URL)
	repository, err := repositoryFromEnvironment(context.Background())
	if err == nil || repository != nil {
		t.Fatalf("expected migration error, got repository=%v err=%v", repository, err)
	}
}

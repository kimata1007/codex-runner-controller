package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/events"
	"github.com/kimata1007/codex-runner-controller/internal/memory"
	"github.com/kimata1007/codex-runner-controller/internal/runnerapi"
	"github.com/kimata1007/codex-runner-controller/internal/runs"
)

type handlerTestRepository struct {
	*memory.Repository
	events []runs.Event
}

func newHandlerTestRepository() *handlerTestRepository {
	return &handlerTestRepository{Repository: memory.NewRepository()}
}

func (r *handlerTestRepository) AppendEvent(_ context.Context, runID, eventType, payload string) (runs.Event, error) {
	event := runs.Event{ID: "event-1", RunID: runID, Sequence: int64(len(r.events) + 1), EventType: eventType, Payload: payload, CreatedAt: time.Now().UTC()}
	r.events = append(r.events, event)
	return event, nil
}

func (r *handlerTestRepository) ListEvents(_ context.Context, runID string, after int64) ([]runs.Event, error) {
	result := make([]runs.Event, 0)
	for _, event := range r.events {
		if event.RunID == runID && event.Sequence > after {
			result = append(result, event)
		}
	}
	return result, nil
}

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
		if len(request.Requests) != 2 || request.Requests[0].Type != "sequence" || request.Requests[0].SQL == "" || request.Requests[1].Type != "close" {
			t.Errorf("unexpected schema request: %+v", request.Requests)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"type":"ok","response":{"type":"sequence"}},{"type":"ok"}]}`))
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

func TestRouterWiresEventIngestAndSSE(t *testing.T) {
	repository := newHandlerTestRepository()
	eventStore, ok := any(repository).(runnerapi.RunEventStore)
	if !ok {
		t.Fatal("test repository does not implement run event storage")
	}
	handler := runnerapi.NewHandler(runnerapi.HandlerDependencies{Repository: repository, EventStore: eventStore, EventHub: events.NewBroker()})
	router := runnerapi.NewRouter(handler)

	request := httptest.NewRequest(http.MethodPost, "/internal/runs/run-1/events", strings.NewReader(`{"eventType":"delta","payload":{"text":"hi"}}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("event status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sseRequest := httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/runs/run-1/events", nil)
	sseRecorder := httptest.NewRecorder()
	router.ServeHTTP(sseRecorder, sseRequest)
	if sseRecorder.Code != http.StatusOK {
		t.Fatalf("SSE status = %d, body = %s", sseRecorder.Code, sseRecorder.Body.String())
	}
	if !strings.Contains(sseRecorder.Body.String(), "event: delta") || !strings.Contains(sseRecorder.Body.String(), `data: {"text":"hi"}`) {
		t.Fatalf("SSE body = %q", sseRecorder.Body.String())
	}
}

func TestGitCredentialsSecretNameFromEnvironment(t *testing.T) {
	t.Setenv("CODEX_GIT_CREDENTIALS_SECRET_NAME", " github-credentials ")
	if got := gitCredentialsSecretNameFromEnvironment(); got != "github-credentials" {
		t.Fatalf("credentials secret name = %q", got)
	}
}

func TestGitCredentialsSecretNameFromEnvironmentWhenUnset(t *testing.T) {
	t.Setenv("CODEX_GIT_CREDENTIALS_SECRET_NAME", "")
	if got := gitCredentialsSecretNameFromEnvironment(); got != "" {
		t.Fatalf("credentials secret name = %q, want empty", got)
	}
}

package events

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/runs"
	"github.com/labstack/echo/v4"
)

type fakeStore struct {
	mu     sync.Mutex
	events []runs.Event
}

func (s *fakeStore) ListEvents(_ context.Context, runID string, after int64) ([]runs.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]runs.Event, 0)
	for _, event := range s.events {
		if event.RunID == runID && event.Sequence > after {
			result = append(result, event)
		}
	}
	return result, nil
}

func TestSSEHandlerReplaysStoredEvents(t *testing.T) {
	store := &fakeStore{events: []runs.Event{{RunID: "run-1", Sequence: 1, EventType: "delta", Payload: `{"text":"hello"}`}}}
	broker := NewBroker()
	router := echo.New()
	router.GET("/v1/runs/:runID/events", NewSSEHandler(store, broker))

	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/runs/run-1/events", nil)
	response := newFlushRecorder()
	done := make(chan struct{})
	go func() {
		router.ServeHTTP(response, request)
		close(done)
	}()

	deadline := time.After(time.Second)
	for !strings.Contains(response.Body.String(), "data: {\"text\":\"hello\"}") {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for stored event")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not stop")
	}
	if !strings.Contains(response.Body.String(), "id: 1\nevent: delta") {
		t.Fatalf("response = %q", response.Body.String())
	}
}

type flushRecorder struct {
	*httptest.ResponseRecorder
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
}

func (r *flushRecorder) Flush() {
	r.ResponseRecorder.Flush()
}

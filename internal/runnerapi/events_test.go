package runnerapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/memory"
	"github.com/kimata1007/codex-runner-controller/internal/runs"
)

type fakeRunEventStore struct {
	event runs.Event
}

func (s *fakeRunEventStore) AppendEvent(_ context.Context, runID, eventType, payload string) (runs.Event, error) {
	s.event = runs.Event{ID: "event-1", RunID: runID, Sequence: 1, EventType: eventType, Payload: payload, CreatedAt: time.Now().UTC()}
	return s.event, nil
}

type fakeRunEventPublisher struct {
	events []runs.Event
}

func (p *fakeRunEventPublisher) Publish(event runs.Event) {
	p.events = append(p.events, event)
}

func TestReceiveRunEventPersistsAndPublishes(t *testing.T) {
	store := &fakeRunEventStore{}
	publisher := &fakeRunEventPublisher{}
	handler := NewHandlerWithDispatcherAndEvents(memory.NewRepository(), nil, store, publisher)
	request := httptest.NewRequest(http.MethodPost, "/internal/runs/run-1/events", strings.NewReader(`{"eventType":"item/updated","payload":{"text":"done"}}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	NewRouter(handler).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if store.event.RunID != "run-1" || store.event.EventType != "item/updated" || store.event.Payload != `{"text":"done"}` {
		t.Fatalf("stored event = %+v", store.event)
	}
	if len(publisher.events) != 1 || publisher.events[0].ID != "event-1" {
		t.Fatalf("published events = %+v", publisher.events)
	}
	var response runs.Event
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Sequence != 1 {
		t.Fatalf("response = %+v", response)
	}
}

func TestReceiveRunEventRequiresEventType(t *testing.T) {
	handler := NewHandlerWithDispatcherAndEvents(memory.NewRepository(), nil, &fakeRunEventStore{}, &fakeRunEventPublisher{})
	request := httptest.NewRequest(http.MethodPost, "/internal/runs/run-1/events", strings.NewReader(`{"payload":{}}`))
	recorder := httptest.NewRecorder()

	NewRouter(handler).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

package runnerapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/conversation"
	"github.com/kimata1007/codex-runner-controller/internal/events"
	"github.com/kimata1007/codex-runner-controller/internal/memory"
	"github.com/kimata1007/codex-runner-controller/internal/runs"
)

func TestHealth(t *testing.T) {
	router := newTestRouter()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("content type = %q, want application/json", contentType)
	}
}

func TestRoutesUseExpectedHTTPMethods(t *testing.T) {
	router := newTestRouter()

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "health rejects post", method: http.MethodPost, path: "/healthz"},
		{name: "conversation collection rejects get", method: http.MethodGet, path: "/v1/conversations"},
		{name: "conversation rejects post", method: http.MethodPost, path: "/v1/conversations/conversation-1"},
		{name: "messages rejects put", method: http.MethodPut, path: "/v1/conversations/conversation-1/messages"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}

func TestCreateConversationAndMessage(t *testing.T) {
	router := newTestRouter()

	createConversationResponse := serveJSON(t, router, http.MethodPost, "/v1/conversations", "")
	if createConversationResponse.Code != http.StatusCreated {
		t.Fatalf("create conversation status = %d, want %d", createConversationResponse.Code, http.StatusCreated)
	}

	var conversation conversationResponse
	decodeResponse(t, createConversationResponse, &conversation)
	if conversation.ID == "" {
		t.Fatal("conversation id is empty")
	}

	createMessageResponse := serveJSON(t, router, http.MethodPost, "/v1/conversations/"+conversation.ID+"/messages", `{"content":"hello","clientMessageId":"client-1"}`)
	if createMessageResponse.Code != http.StatusAccepted {
		t.Fatalf("create message status = %d, want %d", createMessageResponse.Code, http.StatusAccepted)
	}

	duplicateResponse := serveJSON(t, router, http.MethodPost, "/v1/conversations/"+conversation.ID+"/messages", `{"content":"hello","clientMessageId":"client-1"}`)
	if duplicateResponse.Code != http.StatusOK {
		t.Fatalf("duplicate message status = %d, want %d", duplicateResponse.Code, http.StatusOK)
	}

	messagesResponse := serveJSON(t, router, http.MethodGet, "/v1/conversations/"+conversation.ID+"/messages", "")
	if messagesResponse.Code != http.StatusOK {
		t.Fatalf("list messages status = %d, want %d", messagesResponse.Code, http.StatusOK)
	}
	var payload struct {
		Messages []messageResponse `json:"messages"`
	}
	decodeResponse(t, messagesResponse, &payload)
	if len(payload.Messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(payload.Messages))
	}
}

func TestCreateMessageActivatesSessionBeforeRun(t *testing.T) {
	activator := &fakeSessionActivator{}
	router := NewRouter(NewHandler(HandlerDependencies{Repository: memory.NewRepository(), Activator: activator}))
	conversationRecorder := serveJSON(t, router, http.MethodPost, "/v1/conversations", "")
	var value conversationResponse
	decodeResponse(t, conversationRecorder, &value)

	response := serveJSON(t, router, http.MethodPost, "/v1/conversations/"+value.ID+"/messages", `{"content":"hello"}`)
	if response.Code != http.StatusAccepted {
		t.Fatalf("message status = %d, want %d", response.Code, http.StatusAccepted)
	}
	if activator.conversationID != value.ID {
		t.Fatalf("activated conversation = %q, want %q", activator.conversationID, value.ID)
	}
}

func TestCreateMessageReturnsUnavailableWhenSessionActivationFails(t *testing.T) {
	activator := &fakeSessionActivator{err: errors.New("activation failed")}
	repository := memory.NewRepository()
	router := NewRouter(NewHandler(HandlerDependencies{Repository: repository, Activator: activator}))
	conversationRecorder := serveJSON(t, router, http.MethodPost, "/v1/conversations", "")
	var value conversationResponse
	decodeResponse(t, conversationRecorder, &value)

	response := serveJSON(t, router, http.MethodPost, "/v1/conversations/"+value.ID+"/messages", `{"content":"hello"}`)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("message status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

type fakeSessionActivator struct {
	conversationID string
	err            error
}

func (f *fakeSessionActivator) Activate(_ context.Context, conversationID string) error {
	f.conversationID = conversationID
	return f.err
}

func TestMissingConversationReturnsNotFound(t *testing.T) {
	response := serveJSON(t, newTestRouter(), http.MethodGet, "/v1/conversations/missing", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestRepositoryFailureReturnsInternalServerError(t *testing.T) {
	router := NewRouter(NewHandler(HandlerDependencies{Repository: failingRepository{}}))
	response := serveJSON(t, router, http.MethodPost, "/v1/conversations", "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}

type fakeRunEventStore struct {
	event runs.Event
}

func (s *fakeRunEventStore) AppendEvent(_ context.Context, runID, eventType, payload string) (runs.Event, error) {
	s.event = runs.Event{ID: "event-1", RunID: runID, Sequence: 1, EventType: eventType, Payload: payload, CreatedAt: time.Now().UTC()}
	return s.event, nil
}

func (s *fakeRunEventStore) ListEvents(_ context.Context, runID string, after int64) ([]runs.Event, error) {
	if s.event.RunID == runID && s.event.Sequence > after {
		return []runs.Event{s.event}, nil
	}
	return nil, nil
}

type fakeRunEventPublisher struct {
	*events.Broker
	events []runs.Event
}

func (p *fakeRunEventPublisher) Publish(event runs.Event) {
	p.events = append(p.events, event)
	p.Broker.Publish(event)
}

func TestReceiveAppServerRunEventPersistsAndPublishes(t *testing.T) {
	store := &fakeRunEventStore{}
	publisher := &fakeRunEventPublisher{Broker: events.NewBroker()}
	handler := NewHandler(HandlerDependencies{Repository: memory.NewRepository(), EventStore: store, EventHub: publisher})
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

func TestReceiveAppServerRunEventRequiresEventType(t *testing.T) {
	handler := NewHandler(HandlerDependencies{Repository: memory.NewRepository(), EventStore: &fakeRunEventStore{}, EventHub: &fakeRunEventPublisher{Broker: events.NewBroker()}})
	request := httptest.NewRequest(http.MethodPost, "/internal/runs/run-1/events", strings.NewReader(`{"payload":{}}`))
	recorder := httptest.NewRecorder()

	NewRouter(handler).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

type failingRepository struct{}

func (failingRepository) CreateConversation(context.Context) (conversation.Conversation, error) {
	return conversation.Conversation{}, errors.New("database unavailable")
}

func (failingRepository) GetConversation(context.Context, string) (conversation.Conversation, error) {
	return conversation.Conversation{}, errors.New("database unavailable")
}

func (failingRepository) ListMessages(context.Context, string) ([]conversation.Message, error) {
	return nil, errors.New("database unavailable")
}

func (failingRepository) CreateMessage(context.Context, string, conversation.NewMessage) (conversation.Message, bool, error) {
	return conversation.Message{}, false, errors.New("database unavailable")
}

func newTestRouter() http.Handler {
	return NewRouter(NewHandler(HandlerDependencies{Repository: memory.NewRepository()}))
}

func serveJSON(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decodeResponse(t *testing.T, recorder *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.NewDecoder(recorder.Body).Decode(destination); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

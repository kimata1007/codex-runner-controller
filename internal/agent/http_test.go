package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeSession struct {
	started  bool
	resumed  string
	turnID   string
	turnText string
}

func (f *fakeSession) StartThread(context.Context, ThreadStartParams) (Thread, error) {
	f.started = true
	return Thread{ID: "thread-new"}, nil
}

func (f *fakeSession) ResumeThread(_ context.Context, id string, _ ThreadStartParams) (Thread, error) {
	f.resumed = id
	return Thread{ID: id}, nil
}

func (f *fakeSession) StartTurn(_ context.Context, id string, input []TurnInput) error {
	f.turnID = id
	f.turnText = input[0].Text
	return nil
}

func TestHTTPHandlerCreatesResumesAndStartsTurn(t *testing.T) {
	fake := new(fakeSession)
	tracker := NewRunTracker()
	handler := NewHTTPHandlerWithTracker(fake, tracker)

	create := httptest.NewRecorder()
	handler.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/v1/threads", strings.NewReader(`{"model":"default"}`)))
	if create.Code != http.StatusCreated || !fake.started {
		t.Fatalf("create status=%d started=%v body=%s", create.Code, fake.started, create.Body.String())
	}

	resume := httptest.NewRecorder()
	handler.ServeHTTP(resume, httptest.NewRequest(http.MethodPost, "/v1/threads/thread-old/resume", strings.NewReader(`{}`)))
	if resume.Code != http.StatusOK || fake.resumed != "thread-old" {
		t.Fatalf("resume status=%d id=%q", resume.Code, fake.resumed)
	}

	turn := httptest.NewRecorder()
	handler.ServeHTTP(turn, httptest.NewRequest(http.MethodPost, "/v1/threads/thread-old/turns", strings.NewReader(`{"text":"hello","runId":"run-1"}`)))
	if turn.Code != http.StatusAccepted || fake.turnID != "thread-old" || fake.turnText != "hello" || tracker.Current() != "run-1" {
		t.Fatalf("turn status=%d id=%q text=%q", turn.Code, fake.turnID, fake.turnText)
	}
}

func TestHTTPHandlerRejectsBlankTurn(t *testing.T) {
	handler := NewHTTPHandler(new(fakeSession))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/threads/thread-1/turns", strings.NewReader(`{"text":"  "}`)))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body["error"] != "text is required" {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestHTTPHandlerExposesReadinessProbe(t *testing.T) {
	handler := NewHTTPHandler(new(fakeSession))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestHTTPHandlerResolvesApproval(t *testing.T) {
	resolver := &fakeApprovalResolver{}
	handler := NewHTTPHandlerWithTrackerAndApprovals(new(fakeSession), NewRunTracker(), resolver)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/approvals/7", strings.NewReader(`{"decision":"accept"}`)))
	if response.Code != http.StatusAccepted || resolver.requestID != "7" || !strings.Contains(string(resolver.response), "accept") {
		t.Fatalf("status=%d id=%q response=%s", response.Code, resolver.requestID, resolver.response)
	}
}

type fakeApprovalResolver struct {
	requestID string
	response  json.RawMessage
}

func (f *fakeApprovalResolver) Resolve(requestID string, response any) error {
	f.requestID = requestID
	f.response, _ = json.Marshal(response)
	return nil
}

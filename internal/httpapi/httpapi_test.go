package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kimata1007/codex-runner-controller/internal/conversation"
	"github.com/kimata1007/codex-runner-controller/internal/memory"
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

func TestMissingConversationReturnsNotFound(t *testing.T) {
	response := serveJSON(t, newTestRouter(), http.MethodGet, "/v1/conversations/missing", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestRepositoryFailureReturnsInternalServerError(t *testing.T) {
	router := NewRouter(NewHandler(failingRepository{}))
	response := serveJSON(t, router, http.MethodPost, "/v1/conversations", "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
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
	return NewRouter(NewHandler(memory.NewRepository()))
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

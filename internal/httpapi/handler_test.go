package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealth(t *testing.T) {
	server := NewServer()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	server.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("content type = %q, want application/json", contentType)
	}
}

func TestCreateConversationAndMessage(t *testing.T) {
	server := NewServer()
	router := server.Routes()

	conversationResponse := serveJSON(t, router, http.MethodPost, "/v1/conversations", `{"mode":"chat"}`)
	if conversationResponse.Code != http.StatusCreated {
		t.Fatalf("create conversation status = %d, want %d", conversationResponse.Code, http.StatusCreated)
	}

	var conversation Conversation
	decodeResponse(t, conversationResponse, &conversation)
	if conversation.ID == "" {
		t.Fatal("conversation id is empty")
	}

	messageResponse := serveJSON(t, router, http.MethodPost, "/v1/conversations/"+conversation.ID+"/messages", `{"content":"hello","clientMessageId":"client-1"}`)
	if messageResponse.Code != http.StatusAccepted {
		t.Fatalf("create message status = %d, want %d", messageResponse.Code, http.StatusAccepted)
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
		Messages []Message `json:"messages"`
	}
	decodeResponse(t, messagesResponse, &payload)
	if len(payload.Messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(payload.Messages))
	}
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

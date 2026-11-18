package runnerapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kimata1007/codex-runner-controller/internal/agent"
	"github.com/kimata1007/codex-runner-controller/internal/memory"
)

type fakeCodexSettingsReader struct {
	lastConversationID string
}

func (f *fakeCodexSettingsReader) ReadSettings(_ context.Context, conversationID string) (agent.CodexSettings, error) {
	f.lastConversationID = conversationID
	return agent.CodexSettings{ApprovalPolicy: "never", SandboxMode: "workspace-write"}, nil
}

func TestCodexSettingsReadsAuthBrokerWhenConversationIsNotSelected(t *testing.T) {
	reader := new(fakeCodexSettingsReader)
	router := NewRouter(NewHandler(HandlerDependencies{
		Repository: memory.NewRepository(), SettingsReader: reader,
	}))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/codex/settings", nil))
	if response.Code != http.StatusOK || reader.lastConversationID != "" {
		t.Fatalf("status=%d conversationID=%q body=%s", response.Code, reader.lastConversationID, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"source":"auth-broker"`) {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
}

func TestCodexSettingsRejectsUnknownConversation(t *testing.T) {
	reader := new(fakeCodexSettingsReader)
	router := NewRouter(NewHandler(HandlerDependencies{
		Repository: memory.NewRepository(), SettingsReader: reader,
	}))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/codex/settings?conversationId=missing", nil))
	if response.Code != http.StatusNotFound || reader.lastConversationID != "" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

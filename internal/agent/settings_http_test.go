package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeSettingsSession struct {
	*fakeSession
}

func (f *fakeSettingsSession) ReadSettings(context.Context) (CodexSettings, error) {
	return CodexSettings{
		ApprovalPolicy: "never",
		SandboxMode: "workspace-write",
	}, nil
}

func TestSettingsEndpointReturnsSafeProjection(t *testing.T) {
	session := &fakeSettingsSession{fakeSession: new(fakeSession)}
	handler := NewHTTPHandler(session)
	req := httptest.NewRequest(http.MethodGet, "/v1/codex/settings", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"approvalPolicy":"never"`) {
		t.Fatalf("missing settings: %s", rec.Body.String())
	}
}

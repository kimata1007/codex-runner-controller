package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProjectCodexSettingsReturnsOnlyAllowlistedFields(t *testing.T) {
	input := json.RawMessage(`{
		"model": "gpt-6",
		"approval_policy": "never",
		"sandbox_mode": "workspace-write",
		"api_key": "SHOULD_NOT_APPEAR",
		"mcp_servers": {"github": {"env": {"TOKEN": "SHOULD_NOT_APPEAR"}}},
		"model_providers": {"foo": {"env_key": "SECRET"}},
		"apps": {
			"_default": {
				"enabled": true,
				"default_tools_approval_mode": "writes",
				"destructive_enabled": false,
				"tools": {"github/pr-create": {"enabled": false, "approval_mode": "prompt", "token": "SHOULD_NOT_APPEAR"}}
			}
		}
	}`)
	got, err := projectCodexSettings(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "gpt-6" || got.ApprovalPolicy != "never" || got.SandboxMode != "workspace-write" {
		t.Fatalf("unexpected settings: %+v", got)
	}
	if p := got.Apps["_default"]; p.DefaultToolsApprovalMode != "writes" || p.DestructiveEnabled == nil || *p.DestructiveEnabled {
		t.Fatalf("unexpected app policy: %+v", p)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"SHOULD_NOT_APPEAR", "api_key", "mcp_servers", "model_providers", "env_key"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("settings leaked %q: %s", secret, encoded)
		}
	}
}

func TestProjectCodexSettingsRejectsMalformedConfig(t *testing.T) {
	if _, err := projectCodexSettings(json.RawMessage(`{bad`)); err == nil {
		t.Fatal("expected decoding error")
	}
}

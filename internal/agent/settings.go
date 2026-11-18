package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// CodexSettings is an explicit allowlist of non-secret App Server settings.
// Never return config/read's raw response: it can contain credentials,
// provider configuration, MCP environment variables and other private data.
type CodexSettings struct {
	Model          string                    `json:"model,omitempty"`
	ApprovalPolicy string                    `json:"approvalPolicy,omitempty"`
	SandboxMode    string                    `json:"sandboxMode,omitempty"`
	Apps           map[string]CodexAppPolicy `json:"apps,omitempty"`
}

type CodexAppPolicy struct {
	Enabled                  *bool                      `json:"enabled,omitempty"`
	DefaultToolsEnabled      *bool                      `json:"defaultToolsEnabled,omitempty"`
	DefaultToolsApprovalMode string                     `json:"defaultToolsApprovalMode,omitempty"`
	DestructiveEnabled       *bool                      `json:"destructiveEnabled,omitempty"`
	OpenWorldEnabled         *bool                      `json:"openWorldEnabled,omitempty"`
	Tools                    map[string]CodexToolPolicy `json:"tools,omitempty"`
}

type CodexToolPolicy struct {
	Enabled      *bool  `json:"enabled,omitempty"`
	ApprovalMode string `json:"approvalMode,omitempty"`
}

type configReadResult struct {
	Config json.RawMessage `json:"config"`
}

func (s *Session) ReadSettings(ctx context.Context) (CodexSettings, error) {
	if s == nil || s.client == nil {
		return CodexSettings{}, fmt.Errorf("agent session: nil client")
	}
	var result configReadResult
	if err := s.client.Call(ctx, "config/read", map[string]any{"includeLayers": false}, &result); err != nil {
		return CodexSettings{}, fmt.Errorf("read Codex config: %w", err)
	}
	if len(result.Config) == 0 {
		return CodexSettings{}, fmt.Errorf("read Codex config: missing config")
	}
	return projectCodexSettings(result.Config)
}

func projectCodexSettings(config json.RawMessage) (CodexSettings, error) {
	// Decode only fields intended for display; unknown fields are discarded.
	var source struct {
		Model          string                    `json:"model"`
		ApprovalPolicy string                    `json:"approval_policy"`
		SandboxMode    string                    `json:"sandbox_mode"`
		Apps           map[string]json.RawMessage `json:"apps"`
	}
	if err := json.Unmarshal(config, &source); err != nil {
		return CodexSettings{}, fmt.Errorf("decode Codex settings: %w", err)
	}
	settings := CodexSettings{
		Model: source.Model, ApprovalPolicy: source.ApprovalPolicy,
		SandboxMode: source.SandboxMode,
	}
	for name, raw := range source.Apps {
		if len(name) == 0 || len(name) > 128 {
			continue
		}
		var item struct {
			Enabled                  *bool                      `json:"enabled"`
			DefaultToolsEnabled      *bool                      `json:"default_tools_enabled"`
			DefaultToolsApprovalMode string                     `json:"default_tools_approval_mode"`
			DestructiveEnabled       *bool                      `json:"destructive_enabled"`
			OpenWorldEnabled         *bool                      `json:"open_world_enabled"`
			Tools                    map[string]json.RawMessage `json:"tools"`
		}
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		policy := CodexAppPolicy{
			Enabled: item.Enabled, DefaultToolsEnabled: item.DefaultToolsEnabled,
			DefaultToolsApprovalMode: item.DefaultToolsApprovalMode,
			DestructiveEnabled: item.DestructiveEnabled, OpenWorldEnabled: item.OpenWorldEnabled,
		}
		for toolName, toolRaw := range item.Tools {
			if strings.TrimSpace(toolName) == "" || len(toolName) > 128 {
				continue
			}
			var tool CodexToolPolicy
			var fields struct {
				Enabled      *bool  `json:"enabled"`
				ApprovalMode string `json:"approval_mode"`
			}
			if json.Unmarshal(toolRaw, &fields) != nil {
				continue
			}
			tool.Enabled, tool.ApprovalMode = fields.Enabled, fields.ApprovalMode
			if policy.Tools == nil {
				policy.Tools = make(map[string]CodexToolPolicy)
			}
			policy.Tools[toolName] = tool
		}
		if settings.Apps == nil {
			settings.Apps = make(map[string]CodexAppPolicy)
		}
		settings.Apps[name] = policy
	}
	return settings, nil
}

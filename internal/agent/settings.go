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
	InstalledApps  []CodexInstalledApp       `json:"installedApps,omitempty"`
	AppsError      string                    `json:"appsError,omitempty"`
}

// CodexInstalledApp reports effective access and display-only tool summaries.
// This is independent of the user's app configuration overrides.
type CodexInstalledApp struct {
	ID       string         `json:"id"`
	Name     string         `json:"name,omitempty"`
	Enabled  bool           `json:"enabled"`
	Callable bool           `json:"callable"`
	Tools    []CodexAppTool `json:"tools,omitempty"`
}

type CodexAppTool struct {
	Name           string `json:"name"`
	Enabled        bool   `json:"enabled"`
	ReadOnly       bool   `json:"readOnly"`
	DisabledReason string `json:"disabledReason,omitempty"`
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
	settings, err := projectCodexSettings(result.Config)
	if err != nil {
		return CodexSettings{}, err
	}
	// Fetch effective app availability separately. Failure to list apps
	// must not prevent the user from viewing the core Codex settings.
	settings.InstalledApps, err = s.readInstalledApps(ctx)
	if err != nil {
		settings.AppsError = "アプリ一覧またはツール詳細を取得できませんでした"
	}
	return settings, nil
}

func projectCodexSettings(config json.RawMessage) (CodexSettings, error) {
	// Decode only fields intended for display; unknown fields are discarded.
	var source struct {
		Model          string                     `json:"model"`
		ApprovalPolicy string                     `json:"approval_policy"`
		SandboxMode    string                     `json:"sandbox_mode"`
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
			DestructiveEnabled:       item.DestructiveEnabled, OpenWorldEnabled: item.OpenWorldEnabled,
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

func (s *Session) readInstalledApps(ctx context.Context) ([]CodexInstalledApp, error) {
	var installed struct {
		Apps []struct {
			ID          string `json:"id"`
			RuntimeName string `json:"runtimeName"`
			Enabled     bool   `json:"enabled"`
			Callable    bool   `json:"callable"`
		} `json:"apps"`
	}
	if err := s.client.Call(ctx, "app/installed", map[string]any{"forceRefresh": false}, &installed); err != nil {
		return nil, err
	}
	if len(installed.Apps) > 100 {
		installed.Apps = installed.Apps[:100]
	}
	apps := make([]CodexInstalledApp, 0, len(installed.Apps))
	ids := make([]string, 0, len(installed.Apps))
	for _, app := range installed.Apps {
		if len(app.ID) == 0 || len(app.ID) > 128 {
			continue
		}
		apps = append(apps, CodexInstalledApp{
			ID: app.ID, Name: app.RuntimeName, Enabled: app.Enabled, Callable: app.Callable,
		})
		ids = append(ids, app.ID)
	}
	if len(ids) == 0 {
		return apps, nil
	}
	var details struct {
		Apps []struct {
			ID            string `json:"id"`
			ToolSummaries []struct {
				Name           string  `json:"name"`
				IsEnabled      bool    `json:"isEnabled"`
				IsReadOnly     bool    `json:"isReadOnly"`
				DisabledReason *string `json:"disabledReason"`
			} `json:"toolSummaries"`
		} `json:"apps"`
	}
	if err := s.client.Call(ctx, "app/read", map[string]any{
		"appIds": ids, "includeTools": true,
	}, &details); err != nil {
		// Preserve app state, but report that tool details are incomplete.
		return apps, err
	}
	byID := make(map[string]int, len(apps))
	for i := range apps {
		byID[apps[i].ID] = i
	}
	for _, item := range details.Apps {
		index, ok := byID[item.ID]
		if !ok {
			continue
		}
		for _, tool := range item.ToolSummaries {
			if len(tool.Name) == 0 || len(tool.Name) > 128 {
				continue
			}
			summary := CodexAppTool{Name: tool.Name, Enabled: tool.IsEnabled, ReadOnly: tool.IsReadOnly}
			if tool.DisabledReason != nil {
				summary.DisabledReason = *tool.DisabledReason
			}
			apps[index].Tools = append(apps[index].Tools, summary)
		}
	}
	return apps, nil
}

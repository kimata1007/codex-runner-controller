package agentclient

import (
	"context"
	"fmt"
	"strings"

	"github.com/kimata1007/codex-runner-controller/internal/agent"
)

// CodexSettingsClient reads the live Codex App Server through a Runner Agent.
// Without a conversation selection, the always-on Auth Broker Agent is used.
type CodexSettingsClient struct {
	Resolver  EndpointResolver
	BrokerURL string
	Token     string
}

func (c CodexSettingsClient) ReadSettings(ctx context.Context, conversationID string) (agent.CodexSettings, error) {
	endpoint := strings.TrimSpace(c.BrokerURL)
	if conversationID != "" {
		if c.Resolver == nil {
			return agent.CodexSettings{}, fmt.Errorf("session endpoint resolver is unavailable")
		}
		var err error
		endpoint, err = c.Resolver.Resolve(ctx, conversationID)
		if err != nil {
			return agent.CodexSettings{}, fmt.Errorf("resolve Codex session endpoint: %w", err)
		}
	}
	if endpoint == "" {
		return agent.CodexSettings{}, fmt.Errorf("Codex settings endpoint is unavailable")
	}
	var settings agent.CodexSettings
	if err := NewClientWithToken(endpoint, c.Token).request(ctx, "GET", "/v1/codex/settings", nil, &settings); err != nil {
		return agent.CodexSettings{}, fmt.Errorf("read Codex settings from runner-agent: %w", err)
	}
	return settings, nil
}

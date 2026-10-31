package agentclient

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/kimata1007/codex-runner-controller/internal/agent"
	"github.com/kimata1007/codex-runner-controller/internal/runs"
)

type EndpointResolver interface {
	Resolve(context.Context, string) (string, error)
}
type StaticEndpointResolver struct{ URL string }

func (r StaticEndpointResolver) Resolve(_ context.Context, conversationID string) (string, error) {
	if strings.TrimSpace(conversationID) == "" {
		return "", fmt.Errorf("conversation id is required")
	}
	if strings.TrimSpace(r.URL) == "" {
		return "", fmt.Errorf("runner-agent URL is required")
	}
	return strings.TrimRight(r.URL, "/"), nil
}

type RunClientFactory struct {
	resolver   EndpointResolver
	params     agent.ThreadStartParams
	token      string
	httpClient *http.Client
}

func NewRunClientFactory(resolver EndpointResolver, params agent.ThreadStartParams) *RunClientFactory {
	return NewRunClientFactoryWithToken(resolver, params, "")
}
func NewRunClientFactoryWithToken(resolver EndpointResolver, params agent.ThreadStartParams, token string) *RunClientFactory {
	return &RunClientFactory{resolver: resolver, params: params, token: token}
}
func (f *RunClientFactory) WithHTTPClient(client *http.Client) *RunClientFactory {
	if f != nil && client != nil {
		f.httpClient = client
	}
	return f
}
func (f *RunClientFactory) ClientFor(ctx context.Context, conversationID string) (runs.ThreadClient, error) {
	if f == nil || f.resolver == nil {
		return nil, fmt.Errorf("runner-agent endpoint resolver is required")
	}
	endpoint, err := f.resolver.Resolve(ctx, conversationID)
	if err != nil {
		return nil, fmt.Errorf("resolve runner-agent endpoint: %w", err)
	}
	client := NewClientWithToken(endpoint, f.token)
	if f.httpClient != nil {
		client.WithHTTPClient(f.httpClient)
	}
	return NewRunAdapter(client, f.params), nil
}

var _ runs.ThreadClientFactory = (*RunClientFactory)(nil)

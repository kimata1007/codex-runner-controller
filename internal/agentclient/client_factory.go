package agentclient

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/kimata1007/codex-runner-controller/internal/agent"
	"github.com/kimata1007/codex-runner-controller/internal/runs"
)

// EndpointResolver maps a conversation to the stable Service endpoint of its session Pod.
type EndpointResolver interface {
	Resolve(context.Context, string) (string, error)
}

// StaticEndpointResolver preserves the single-agent development setup.
type StaticEndpointResolver struct{ URL string }

func (r StaticEndpointResolver) Resolve(_ context.Context, conversationID string) (string, error) {
	if strings.TrimSpace(conversationID) == "" {
		return "", fmt.Errorf("conversation id is required")
	}
	if strings.TrimSpace(r.URL) == "" {
		return "", fmt.Errorf("runner-agent URL is required")
	}
	return r.URL, nil
}

// RunClientFactory creates a runner-agent HTTP adapter for each conversation.
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
	if client != nil {
		f.httpClient = client
	}
	return f
}
func (f *RunClientFactory) CreateClientForConversation(ctx context.Context, conversationID string) (runs.ThreadClient, error) {
	client, err := f.CreateAccountClientForConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	return NewRunAdapter(client, f.params), nil
}

func (f *RunClientFactory) CreateAccountClientForConversation(ctx context.Context, conversationID string) (agent.AccountClient, error) {
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
	return client, nil
}

var _ runs.ThreadClientFactory = (*RunClientFactory)(nil)

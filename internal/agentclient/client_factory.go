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

// ServiceEndpointResolver resolves the stable Kubernetes Service created for
// each CodexSession. The corresponding CodexSession must be Ready first.
type ServiceEndpointResolver struct {
	Namespace string
	Port      int
}

func (r ServiceEndpointResolver) Resolve(_ context.Context, conversationID string) (string, error) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return "", fmt.Errorf("conversation id is required")
	}
	namespace := strings.TrimSpace(r.Namespace)
	if namespace == "" {
		namespace = "codex-system"
	}
	port := r.Port
	if port == 0 {
		port = 8081
	}
	return fmt.Sprintf("http://%s.%s.svc:%d", conversationID, namespace, port), nil
}

// RunClientFactory creates a runner-agent HTTP adapter for each conversation.
type RunClientFactory struct {
	resolver   EndpointResolver
	params     agent.ThreadStartParams
	httpClient *http.Client
}

func NewRunClientFactory(resolver EndpointResolver, params agent.ThreadStartParams) *RunClientFactory {
	return &RunClientFactory{resolver: resolver, params: params}
}

func (f *RunClientFactory) WithHTTPClient(client *http.Client) *RunClientFactory {
	if client != nil {
		f.httpClient = client
	}
	return f
}

func (f *RunClientFactory) CreateClientForConversation(ctx context.Context, conversationID string) (runs.ThreadClient, error) {
	if f == nil || f.resolver == nil {
		return nil, fmt.Errorf("runner-agent endpoint resolver is required")
	}
	endpoint, err := f.resolver.Resolve(ctx, conversationID)
	if err != nil {
		return nil, fmt.Errorf("resolve runner-agent endpoint: %w", err)
	}
	client := NewClient(endpoint)
	if f.httpClient != nil {
		client.WithHTTPClient(f.httpClient)
	}
	return NewRunAdapter(client, f.params), nil
}

var _ runs.ThreadClientFactory = (*RunClientFactory)(nil)

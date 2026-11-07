package agentclient

import (
	"context"

	"github.com/kimata1007/codex-runner-controller/internal/agent"
	"github.com/kimata1007/codex-runner-controller/internal/runs"
)

// RunAdapter adapts the runner-agent HTTP client to runs.ThreadClient.
// Thread options are session configuration, not per-message input.
type RunAdapter struct {
	client *Client
	params agent.ThreadStartParams
}

func NewRunAdapter(client *Client, params agent.ThreadStartParams) *RunAdapter {
	return &RunAdapter{client: client, params: params}
}

func (c *RunAdapter) StartThread(ctx context.Context) (string, error) {
	thread, err := c.client.StartThread(ctx, c.params)
	if err != nil {
		return "", err
	}
	return thread.ID, nil
}

func (c *RunAdapter) ResumeThread(ctx context.Context, threadID string) error {
	_, err := c.client.ResumeThread(ctx, threadID, c.params)
	return err
}

func (c *RunAdapter) StartTurn(ctx context.Context, threadID, runID, text string) error {
	return c.client.StartTurnForRun(ctx, threadID, runID, []agent.TurnInput{{Type: "text", Text: text}})
}

var _ runs.ThreadClient = (*RunAdapter)(nil)

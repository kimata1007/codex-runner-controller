package agent

import (
	"context"

	"github.com/kimata1007/codex-runner-controller/internal/runs"
)

// RunClient adapts the runner-agent session API to runs.ThreadClient.
// Thread options are session configuration, not per-message input.
type RunClient struct {
	client *RemoteClient
	params ThreadStartParams
}

func NewRunClient(client *RemoteClient, params ThreadStartParams) *RunClient {
	return &RunClient{client: client, params: params}
}

func (c *RunClient) StartThread(ctx context.Context) (string, error) {
	thread, err := c.client.StartThread(ctx, c.params)
	if err != nil {
		return "", err
	}
	return thread.ID, nil
}

func (c *RunClient) ResumeThread(ctx context.Context, threadID string) error {
	_, err := c.client.ResumeThread(ctx, threadID, c.params)
	return err
}

func (c *RunClient) StartTurn(ctx context.Context, threadID, text string) error {
	return c.client.StartTurn(ctx, threadID, []TurnInput{{Type: "text", Text: text}})
}

var _ runs.ThreadClient = (*RunClient)(nil)

package runs

import (
	"context"
	"fmt"
	"strings"
)

// ThreadClient is the runner-agent boundary used to prepare a thread and send a turn.
type ThreadClient interface {
	StartThread(context.Context) (string, error)
	ResumeThread(context.Context, string) error
	StartTurn(context.Context, string, string, string) error
}

type ApprovalClient interface {
	RespondApproval(context.Context, string, any) error
}

// ThreadClientFactory selects the runner-agent client for a conversation.
type ThreadClientFactory interface {
	CreateClientForConversation(context.Context, string) (ThreadClient, error)
}

// Store persists the run lifecycle. Event persistence is handled by the App
// Server event consumer in a separate path.
type Store interface {
	CreateRun(context.Context, string, string, string) (Run, error)
	GetRun(context.Context, string) (Run, bool, error)
	SetRunStatus(context.Context, string, string, string, string) error
}

type ThreadStore interface {
	LatestThreadID(context.Context, string) (string, bool, error)
}

// MessageDispatcher sends one persisted user message as a Codex turn and
// records the lifecycle of the corresponding Run. It uses an existing
// runner-agent; it does not start a Pod or a Codex App Server process.
type MessageDispatcher struct {
	store   Store
	factory ThreadClientFactory
}

func NewMessageDispatcher(store Store, client ThreadClient) *MessageDispatcher {
	return NewMessageDispatcherWithFactory(store, staticClientFactory{client: client})
}

func NewMessageDispatcherWithFactory(store Store, factory ThreadClientFactory) *MessageDispatcher {
	return &MessageDispatcher{store: store, factory: factory}
}

// Dispatch creates or resumes the conversation thread, records a Run, then
// sends one turn. It returns after the runner-agent accepts turn/start; streamed
// output is handled separately by the event consumer.
func (d *MessageDispatcher) Dispatch(ctx context.Context, conversationID, messageID, threadID, text string) (Run, error) {
	if d == nil || d.store == nil || d.factory == nil {
		return Run{}, fmt.Errorf("message dispatcher: store and client are required")
	}
	if strings.TrimSpace(conversationID) == "" || strings.TrimSpace(messageID) == "" {
		return Run{}, fmt.Errorf("message dispatcher: conversation and message are required")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Run{}, fmt.Errorf("message dispatcher: text is required")
	}

	client, err := d.factory.CreateClientForConversation(ctx, conversationID)
	if err != nil {
		return Run{}, fmt.Errorf("message dispatcher: select session client: %w", err)
	}
	if client == nil {
		return Run{}, fmt.Errorf("message dispatcher: session client is required")
	}
	if threadID == "" {
		if threadStore, ok := d.store.(ThreadStore); ok {
			storedThreadID, found, err := threadStore.LatestThreadID(ctx, conversationID)
			if err != nil {
				return Run{}, fmt.Errorf("message dispatcher: find conversation thread: %w", err)
			}
			if found {
				threadID = storedThreadID
			}
		}
	}

	threadID, err = d.prepareThread(ctx, client, threadID)
	if err != nil {
		return Run{}, err
	}
	run, err := d.recordRun(ctx, conversationID, messageID, threadID)
	if err != nil {
		return Run{}, err
	}
	if err := d.sendTurn(ctx, client, run.ID, threadID, text); err != nil {
		return Run{}, err
	}
	return run, nil
}

func (d *MessageDispatcher) RespondApproval(ctx context.Context, runID, requestID string, response any) error {
	if d == nil || d.store == nil || d.factory == nil {
		return fmt.Errorf("message dispatcher: run store and client factory are required")
	}
	run, ok, err := d.store.GetRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("message dispatcher: get run: %w", err)
	}
	if !ok {
		return fmt.Errorf("message dispatcher: run %q not found", runID)
	}
	client, err := d.factory.CreateClientForConversation(ctx, run.ConversationID)
	if err != nil {
		return fmt.Errorf("message dispatcher: select session client: %w", err)
	}
	approvalClient, ok := client.(ApprovalClient)
	if !ok {
		return fmt.Errorf("message dispatcher: approval client is not configured")
	}
	if err := approvalClient.RespondApproval(ctx, requestID, response); err != nil {
		return fmt.Errorf("message dispatcher: respond approval: %w", err)
	}
	if approvalStore, ok := d.store.(ApprovalStore); ok {
		if err := approvalStore.ResolveApproval(ctx, runID, requestID, ApprovalStatus(response), response); err != nil {
			return fmt.Errorf("message dispatcher: persist approval response: %w", err)
		}
	}
	return nil
}

func (d *MessageDispatcher) prepareThread(ctx context.Context, client ThreadClient, threadID string) (string, error) {
	if threadID == "" {
		newThreadID, err := client.StartThread(ctx)
		if err != nil {
			return "", fmt.Errorf("message dispatcher: start thread: %w", err)
		}
		threadID = newThreadID
	} else if err := client.ResumeThread(ctx, threadID); err != nil {
		return "", fmt.Errorf("message dispatcher: resume thread: %w", err)
	}
	if strings.TrimSpace(threadID) == "" {
		return "", fmt.Errorf("message dispatcher: thread id is required")
	}
	return threadID, nil
}

func (d *MessageDispatcher) recordRun(ctx context.Context, conversationID, messageID, threadID string) (Run, error) {
	run, err := d.store.CreateRun(ctx, conversationID, messageID, threadID)
	if err != nil {
		return Run{}, fmt.Errorf("message dispatcher: create run: %w", err)
	}
	if err := d.store.SetRunStatus(ctx, run.ID, StatusRunning, "", ""); err != nil {
		return Run{}, fmt.Errorf("message dispatcher: mark run running: %w", err)
	}
	run.Status = StatusRunning
	return run, nil
}

func (d *MessageDispatcher) sendTurn(ctx context.Context, client ThreadClient, runID, threadID, text string) error {
	if err := client.StartTurn(ctx, threadID, runID, text); err != nil {
		_ = d.store.SetRunStatus(ctx, runID, StatusFailed, "turn_start_failed", err.Error())
		return fmt.Errorf("message dispatcher: send turn: %w", err)
	}
	return nil
}

type staticClientFactory struct{ client ThreadClient }

func (f staticClientFactory) CreateClientForConversation(context.Context, string) (ThreadClient, error) {
	if f.client == nil {
		return nil, fmt.Errorf("static session client is required")
	}
	return f.client, nil
}

func (d *MessageDispatcher) InterruptRun(ctx context.Context, runID string) error {
	if d == nil || d.store == nil || d.factory == nil {
		return fmt.Errorf("interrupt run: dispatcher unavailable")
	}
	run, found, err := d.store.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("interrupt run: run not found")
	}
	if run.Status != StatusRunning && run.Status != StatusWaitingApproval {
		return fmt.Errorf("interrupt run: run is not active")
	}
	client, err := d.factory.CreateClientForConversation(ctx, run.ConversationID)
	if err != nil {
		return err
	}
	interrupter, ok := client.(interface {
		InterruptRun(context.Context, string) error
	})
	if !ok {
		return fmt.Errorf("interrupt run: client does not support interruption")
	}
	return interrupter.InterruptRun(ctx, runID)
}

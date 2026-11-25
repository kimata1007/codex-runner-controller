package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/runs"
)

func (r *Repository) LatestThreadID(ctx context.Context, conversationID string) (string, bool, error) {
	result, err := r.client.Exec(ctx, `SELECT thread_id FROM runs
		WHERE conversation_id = $1 AND thread_id IS NOT NULL
		ORDER BY created_at DESC LIMIT 1`, []Value{Text(conversationID)}, true)
	if err != nil {
		return "", false, err
	}
	if len(result.Rows) == 0 || len(result.Rows[0]) == 0 || result.Rows[0][0].Type == "null" || result.Rows[0][0].Value == "" {
		return "", false, nil
	}
	return result.Rows[0][0].Value, true, nil
}

// ListActiveRuns only returns runs belonging to the authenticated user and an
// unarchived conversation. The caller verifies the conversation separately too.
func (r *Repository) ListActiveRuns(ctx context.Context, conversationID string) ([]runs.ActiveRun, error) {
	result, err := r.client.Exec(ctx, `SELECT runs.id, runs.status
		FROM runs JOIN conversations ON conversations.id = runs.conversation_id
		WHERE runs.conversation_id = $1 AND conversations.user_id = $2
		AND conversations.status != 'archived'
		AND runs.status IN ('running', 'waiting_approval')
		ORDER BY runs.created_at`, []Value{Text(conversationID), Text(r.userID)}, true)
	if err != nil {
		return nil, err
	}
	active := make([]runs.ActiveRun, 0, len(result.Rows))
	for _, row := range result.Rows {
		if len(row) < 2 {
			return nil, fmt.Errorf("postgres: active run row has %d columns", len(row))
		}
		active = append(active, runs.ActiveRun{ID: row[0].Value, Status: row[1].Value})
	}
	return active, nil
}

func (r *Repository) CreateRun(ctx context.Context, conversationID, messageID, threadID string) (runs.Run, error) {
	now := time.Now().UTC()
	run := runs.Run{
		ID:             r.newID("run"),
		ConversationID: conversationID,
		MessageID:      messageID,
		ThreadID:       threadID,
		Status:         runs.StatusQueued,
		CreatedAt:      now,
	}
	_, err := r.client.Exec(ctx, `INSERT INTO runs
		(id, conversation_id, message_id, thread_id, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`, []Value{
		Text(run.ID), Text(run.ConversationID), Text(run.MessageID), nullableText(run.ThreadID), Text(run.Status), Text(now.Format(time.RFC3339Nano)),
	}, false)
	if err != nil {
		return runs.Run{}, err
	}
	return run, nil
}

func (r *Repository) SetRunStatus(ctx context.Context, runID, status, errorCode, errorMessage string) error {
	completedAt := Null()
	if status == runs.StatusCompleted || status == runs.StatusFailed || status == runs.StatusInterrupted {
		completedAt = Text(time.Now().UTC().Format(time.RFC3339Nano))
	}
	_, err := r.client.Exec(ctx, `UPDATE runs SET status = $1, completed_at = $2, error_code = $3, error_message = $4 WHERE id = $5`, []Value{
		Text(status), completedAt, nullableText(errorCode), nullableText(errorMessage), Text(runID),
	}, false)
	return err
}

func (r *Repository) GetRun(ctx context.Context, runID string) (runs.Run, bool, error) {
	result, err := r.client.Exec(ctx, `SELECT id, conversation_id, message_id, thread_id, status, created_at, started_at, completed_at, error_code, error_message FROM runs WHERE id = $1`, []Value{Text(runID)}, true)
	if err != nil {
		return runs.Run{}, false, err
	}
	if len(result.Rows) == 0 {
		return runs.Run{}, false, nil
	}
	row := result.Rows[0]
	if len(row) < 10 {
		return runs.Run{}, false, fmt.Errorf("postgres: run row has %d columns", len(row))
	}
	createdAt, err := parseTime(row[5])
	if err != nil {
		return runs.Run{}, false, err
	}
	run := runs.Run{ID: row[0].Value, ConversationID: row[1].Value, MessageID: row[2].Value, Status: row[4].Value, CreatedAt: createdAt}
	if row[3].Type != "null" {
		run.ThreadID = row[3].Value
	}
	if row[6].Type != "null" {
		value, parseErr := parseTime(row[6])
		if parseErr != nil {
			return runs.Run{}, false, parseErr
		}
		run.StartedAt = &value
	}
	if row[7].Type != "null" {
		value, parseErr := parseTime(row[7])
		if parseErr != nil {
			return runs.Run{}, false, parseErr
		}
		run.CompletedAt = &value
	}
	if row[8].Type != "null" {
		run.ErrorCode = row[8].Value
	}
	if row[9].Type != "null" {
		run.ErrorMessage = row[9].Value
	}
	return run, true, nil
}

func (r *Repository) ApplyAppServerEvent(ctx context.Context, runID, eventType, payload string) error {
	status, ok := runs.StatusForEvent(eventType, payload)
	if !ok {
		return nil
	}
	errorCode, errorMessage := "", ""
	if status == runs.StatusFailed {
		errorCode = eventType
		errorMessage = runs.ErrorMessageForEvent(eventType, payload)
	}
	if err := r.SetRunStatus(ctx, runID, status, errorCode, errorMessage); err != nil {
		return err
	}
	messageStatus := "running"
	if status == runs.StatusCompleted {
		messageStatus = "completed"
	} else if status == runs.StatusFailed || status == runs.StatusInterrupted {
		messageStatus = "failed"
	}
	_, err := r.client.Exec(ctx, `UPDATE messages SET status = $1 WHERE id = (SELECT message_id FROM runs WHERE id = $2)`, []Value{Text(messageStatus), Text(runID)}, false)
	if err != nil {
		return err
	}
	if status == runs.StatusCompleted {
		if err := r.persistAssistantMessage(ctx, runID); err != nil {
			return err
		}
	}
	if status == runs.StatusWaitingApproval {
		if err := r.persistApproval(ctx, runID, payload); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) persistApproval(ctx context.Context, runID, payload string) error {
	var request struct {
		RequestID string          `json:"requestId"`
		Method    string          `json:"method"`
		Params    json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal([]byte(payload), &request); err != nil || strings.TrimSpace(request.RequestID) == "" {
		return nil
	}
	params := strings.TrimSpace(string(request.Params))
	if params == "" {
		params = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := r.client.Exec(ctx, `INSERT INTO approvals
		(id, run_id, request_id, action_type, summary, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT(run_id, request_id) DO UPDATE SET action_type = excluded.action_type, summary = excluded.summary`, []Value{
		Text("approval-" + runID + "-" + request.RequestID), Text(runID), Text(request.RequestID), Text(request.Method), Text(params), Text(runs.ApprovalPending), Text(now),
	}, false)
	return err
}

func (r *Repository) RecordApproval(ctx context.Context, runID, requestID, actionType, summary string) error {
	payload, err := json.Marshal(struct {
		RequestID string `json:"requestId"`
		Method    string `json:"method"`
		Params    string `json:"params"`
	}{RequestID: requestID, Method: actionType, Params: summary})
	if err != nil {
		return err
	}
	return r.persistApproval(ctx, runID, string(payload))
}

func (r *Repository) ResolveApproval(ctx context.Context, runID, requestID, status string, response any) error {
	payload, err := json.Marshal(response)
	if err != nil {
		return err
	}
	_, err = r.client.Exec(ctx, `UPDATE approvals SET status = $1, response = $2, resolved_at = $3 WHERE run_id = $4 AND request_id = $5`, []Value{
		Text(status), Text(string(payload)), Text(time.Now().UTC().Format(time.RFC3339Nano)), Text(runID), Text(requestID),
	}, false)
	return err
}

func (r *Repository) ListPendingApprovals(ctx context.Context, conversationID string) ([]runs.Approval, error) {
	result, err := r.client.Exec(ctx, `SELECT a.run_id, a.request_id, a.action_type, a.summary
		FROM approvals a
		JOIN runs r ON r.id = a.run_id
		JOIN conversations c ON c.id = r.conversation_id
		WHERE c.id = $1 AND c.user_id = $2 AND a.status = 'pending'
		ORDER BY a.created_at, a.request_id`, []Value{Text(conversationID), Text(r.userID)}, true)
	if err != nil {
		return nil, err
	}
	approvals := make([]runs.Approval, 0, len(result.Rows))
	for _, row := range result.Rows {
		if len(row) < 4 {
			return nil, fmt.Errorf("postgres: pending approval row has %d columns", len(row))
		}
		params := json.RawMessage(row[3].Value)
		if len(params) == 0 || !json.Valid(params) {
			params = json.RawMessage(`{}`)
		}
		approvals = append(approvals, runs.Approval{
			RunID: row[0].Value, RequestID: row[1].Value, Method: row[2].Value, Params: params,
		})
	}
	return approvals, nil
}

func (r *Repository) persistAssistantMessage(ctx context.Context, runID string) error {
	run, found, err := r.GetRun(ctx, runID)
	if err != nil || !found {
		return err
	}
	events, err := r.ListEvents(ctx, runID, 0)
	if err != nil {
		return err
	}
	content := runs.AssistantTextFromEvents(events)
	if content == "" {
		return nil
	}
	messageID := "assistant-" + runID
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = r.client.Exec(ctx, `INSERT INTO messages
		(id, conversation_id, role, content, sequence, status, created_at)
		VALUES ($1, $2, 'assistant', $3, COALESCE((SELECT MAX(sequence) FROM messages WHERE conversation_id = $4), 0) + 1, 'completed', $5)
		ON CONFLICT(id) DO UPDATE SET content = excluded.content, status = excluded.status, created_at = excluded.created_at`, []Value{
		Text(messageID), Text(run.ConversationID), Text(content), Text(run.ConversationID), Text(now),
	}, false)
	return err
}

func (r *Repository) AppendEvent(ctx context.Context, runID, eventType, payload string) (runs.Event, error) {
	now := time.Now().UTC()
	event := runs.Event{ID: r.newID("event"), RunID: runID, EventType: eventType, Payload: payload, CreatedAt: now}
	_, err := r.client.Exec(ctx, `INSERT INTO events
		(id, run_id, sequence, event_type, payload, created_at)
		VALUES ($1, $2, COALESCE((SELECT MAX(sequence) FROM events WHERE run_id = $3), 0) + 1, $4, $5, $6)`, []Value{
		Text(event.ID), Text(event.RunID), Text(event.RunID), Text(event.EventType), Text(event.Payload), Text(now.Format(time.RFC3339Nano)),
	}, false)
	if err != nil {
		return runs.Event{}, err
	}
	result, err := r.client.Exec(ctx, `SELECT sequence FROM events WHERE id = $1`, []Value{Text(event.ID)}, true)
	if err != nil {
		return runs.Event{}, err
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 1 {
		return runs.Event{}, fmt.Errorf("postgres: event %s sequence was not returned", event.ID)
	}
	sequence, err := strconv.ParseInt(result.Rows[0][0].Value, 10, 64)
	if err != nil {
		return runs.Event{}, fmt.Errorf("postgres: parse event sequence: %w", err)
	}
	event.Sequence = sequence
	return event, nil
}

func (r *Repository) ListEvents(ctx context.Context, runID string, afterSequence int64) ([]runs.Event, error) {
	result, err := r.client.Exec(ctx, `SELECT id, run_id, sequence, event_type, payload, created_at
		FROM events WHERE run_id = $1 AND sequence > $2 ORDER BY sequence`, []Value{Text(runID), Integer(afterSequence)}, true)
	if err != nil {
		return nil, err
	}
	events := make([]runs.Event, 0, len(result.Rows))
	for _, row := range result.Rows {
		if len(row) < 6 {
			return nil, fmt.Errorf("postgres: event row has %d columns", len(row))
		}
		sequence, err := strconv.ParseInt(row[2].Value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("postgres: parse event sequence: %w", err)
		}
		createdAt, err := parseTime(row[5])
		if err != nil {
			return nil, err
		}
		events = append(events, runs.Event{ID: row[0].Value, RunID: row[1].Value, Sequence: sequence, EventType: row[3].Value, Payload: row[4].Value, CreatedAt: createdAt})
	}
	return events, nil
}

func nullableText(value string) Value {
	if value == "" {
		return Null()
	}
	return Text(value)
}

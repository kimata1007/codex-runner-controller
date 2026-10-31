package libsql

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/runs"
)

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
		VALUES (?, ?, ?, ?, ?, ?)`, []Value{
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
	_, err := r.client.Exec(ctx, `UPDATE runs SET status = ?, completed_at = ?, error_code = ?, error_message = ? WHERE id = ?`, []Value{
		Text(status), completedAt, nullableText(errorCode), nullableText(errorMessage), Text(runID),
	}, false)
	return err
}

func (r *Repository) GetRun(ctx context.Context, runID string) (runs.Run, bool, error) {
	result, err := r.client.Exec(ctx, `SELECT id, conversation_id, message_id, thread_id, status, created_at, started_at, completed_at, error_code, error_message FROM runs WHERE id = ?`, []Value{Text(runID)}, true)
	if err != nil {
		return runs.Run{}, false, err
	}
	if len(result.Rows) == 0 {
		return runs.Run{}, false, nil
	}
	row := result.Rows[0]
	if len(row) < 10 {
		return runs.Run{}, false, fmt.Errorf("libsql: run row has %d columns", len(row))
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
	_, err := r.client.Exec(ctx, `UPDATE messages SET status = ? WHERE id = (SELECT message_id FROM runs WHERE id = ?)`, []Value{Text(messageStatus), Text(runID)}, false)
	return err
}

func (r *Repository) AppendEvent(ctx context.Context, runID, eventType, payload string) (runs.Event, error) {
	now := time.Now().UTC()
	event := runs.Event{ID: r.newID("event"), RunID: runID, EventType: eventType, Payload: payload, CreatedAt: now}
	_, err := r.client.Exec(ctx, `INSERT INTO events
		(id, run_id, sequence, event_type, payload, created_at)
		VALUES (?, ?, COALESCE((SELECT MAX(sequence) FROM events WHERE run_id = ?), 0) + 1, ?, ?, ?)`, []Value{
		Text(event.ID), Text(event.RunID), Text(event.RunID), Text(event.EventType), Text(event.Payload), Text(now.Format(time.RFC3339Nano)),
	}, false)
	if err != nil {
		return runs.Event{}, err
	}
	result, err := r.client.Exec(ctx, `SELECT sequence FROM events WHERE id = ?`, []Value{Text(event.ID)}, true)
	if err != nil {
		return runs.Event{}, err
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 1 {
		return runs.Event{}, fmt.Errorf("libsql: event %s sequence was not returned", event.ID)
	}
	sequence, err := strconv.ParseInt(result.Rows[0][0].Value, 10, 64)
	if err != nil {
		return runs.Event{}, fmt.Errorf("libsql: parse event sequence: %w", err)
	}
	event.Sequence = sequence
	return event, nil
}

func (r *Repository) ListEvents(ctx context.Context, runID string, afterSequence int64) ([]runs.Event, error) {
	result, err := r.client.Exec(ctx, `SELECT id, run_id, sequence, event_type, payload, created_at
		FROM events WHERE run_id = ? AND sequence > ? ORDER BY sequence`, []Value{Text(runID), Integer(afterSequence)}, true)
	if err != nil {
		return nil, err
	}
	events := make([]runs.Event, 0, len(result.Rows))
	for _, row := range result.Rows {
		if len(row) < 6 {
			return nil, fmt.Errorf("libsql: event row has %d columns", len(row))
		}
		sequence, err := strconv.ParseInt(row[2].Value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("libsql: parse event sequence: %w", err)
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

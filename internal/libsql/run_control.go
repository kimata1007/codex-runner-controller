package libsql

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/runs"
)

// BindControlRun records the Pod selected by Runner API before sending a turn.
// An existing binding can be reused by a retry on the same Pod.
func (r *Repository) BindControlRun(ctx context.Context, conversationID, runID, podUID string) error {
	if conversationID == "" || runID == "" || podUID == "" {
		return errors.New("incomplete run control binding")
	}
	result, err := r.client.Exec(ctx, `INSERT INTO run_controls (run_id, pod_uid)
		SELECT r.id, ? FROM runs r JOIN conversations c ON c.id = r.conversation_id
		WHERE r.id = ? AND c.id = ? AND c.user_id = ? AND c.status != 'archived' AND r.status = 'running'
		ON CONFLICT(run_id) DO UPDATE SET pod_uid = excluded.pod_uid WHERE run_controls.pod_uid = excluded.pod_uid
		RETURNING run_id`, []Value{Text(podUID), Text(runID), Text(conversationID), Text(r.userID)}, true)
	if err != nil {
		return err
	}
	if len(result.Rows) != 1 {
		return errors.New("run is unavailable for this session")
	}
	return nil
}

func (r *Repository) ActiveControlBindings(ctx context.Context) ([]runs.ControlBinding, error) {
	result, err := r.client.Exec(ctx, `SELECT DISTINCT c.id, rc.pod_uid
		FROM run_controls rc JOIN runs r ON r.id = rc.run_id JOIN conversations c ON c.id = r.conversation_id
		WHERE r.status IN ('running', 'waiting_approval') AND c.status != 'archived' AND c.user_id = ?`,
		[]Value{Text(r.userID)}, true)
	if err != nil {
		return nil, err
	}
	bindings := make([]runs.ControlBinding, 0, len(result.Rows))
	for _, row := range result.Rows {
		if len(row) != 2 {
			return nil, errors.New("invalid run control binding")
		}
		bindings = append(bindings, runs.ControlBinding{ConversationID: row[0].Value, PodUID: row[1].Value})
	}
	return bindings, nil
}

// AppendControlEvent persists only the next event on the bound run. A repeated
// frame must match the already stored event exactly.
func (r *Repository) AppendControlEvent(ctx context.Context, conversationID, podUID, runID string, sequence int64, eventType, payload string) (runs.Event, error) {
	if sequence < 1 {
		return runs.Event{}, errors.New("invalid control event sequence")
	}
	id := "control-" + runID + "-" + strconv.FormatInt(sequence, 10)
	now := time.Now().UTC()
	_, err := r.client.Exec(ctx, `INSERT INTO events (id, run_id, sequence, event_type, payload, created_at)
		SELECT ?, r.id, ?, ?, ?, ? FROM run_controls rc JOIN runs r ON r.id = rc.run_id
		JOIN conversations c ON c.id = r.conversation_id
		WHERE r.id = ? AND rc.pod_uid = ? AND c.id = ? AND c.user_id = ? AND c.status != 'archived'
			AND r.status IN ('running', 'waiting_approval')
			AND COALESCE((SELECT MAX(sequence) FROM events WHERE run_id = r.id), 0) = ? - 1
		ON CONFLICT DO NOTHING`, []Value{Text(id), Integer(sequence), Text(eventType), Text(payload), Text(now.Format(time.RFC3339Nano)),
		Text(runID), Text(podUID), Text(conversationID), Text(r.userID), Integer(sequence)}, false)
	if err != nil {
		return runs.Event{}, err
	}
	result, err := r.client.Exec(ctx, `SELECT e.created_at FROM events e JOIN run_controls rc ON rc.run_id = e.run_id
		JOIN runs r ON r.id = rc.run_id JOIN conversations c ON c.id = r.conversation_id
		WHERE e.id = ? AND e.run_id = ? AND e.sequence = ? AND e.event_type = ? AND e.payload = ?
			AND rc.pod_uid = ? AND c.id = ? AND c.user_id = ? AND c.status != 'archived'`,
		[]Value{Text(id), Text(runID), Integer(sequence), Text(eventType), Text(payload), Text(podUID), Text(conversationID), Text(r.userID)}, true)
	if err != nil {
		return runs.Event{}, err
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 1 {
		return runs.Event{}, errors.New("control event does not match the run binding or expected sequence")
	}
	createdAt, err := parseTime(result.Rows[0][0])
	if err != nil {
		return runs.Event{}, err
	}
	return runs.Event{ID: id, RunID: runID, Sequence: sequence, EventType: eventType, Payload: payload, CreatedAt: createdAt}, nil
}

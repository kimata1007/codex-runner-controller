package agent

import "sync"

// RunTracker associates the currently active App Server turn with its run.
// A session Pod processes one turn at a time, so the latest run ID is enough
// to label asynchronous App Server notifications.
type RunTracker struct {
	mu       sync.RWMutex
	runID    string
	turnID   string
	threadID string
}

func NewRunTracker() *RunTracker {
	return &RunTracker{}
}

func (t *RunTracker) Set(runID string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.runID = runID
	t.turnID, t.threadID = "", ""
	t.mu.Unlock()
}

// Begin claims the session's single active turn. It returns false when another
// turn is already being processed.
func (t *RunTracker) Begin(runID string) bool {
	if t == nil || runID == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.runID != "" {
		return false
	}
	t.runID = runID
	return true
}

func (t *RunTracker) Current() string {
	if t == nil {
		return ""
	}
	t.mu.RLock()
	runID := t.runID
	t.mu.RUnlock()
	return runID
}

// Clear removes the active run only when it still matches runID. This avoids
// a late terminal notification from an older turn clearing a newer turn.
func (t *RunTracker) Clear(runID string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.runID == runID {
		t.runID = ""
		t.turnID, t.threadID = "", ""
	}
	t.mu.Unlock()
}

// RecordTurn associates the App Server identity with the current run.
func (t *RunTracker) RecordTurn(runID, threadID, turnID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if runID != "" && t.runID == runID && threadID != "" && turnID != "" {
		t.threadID, t.turnID = threadID, turnID
	}
}

func (t *RunTracker) Turn(runID string) (string, string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.threadID, t.turnID, runID != "" && t.runID == runID && t.turnID != ""
}

package agent

import "sync"

// RunTracker associates the currently active App Server turn with its run.
// A session Pod processes one turn at a time, so the latest run ID is enough
// to label asynchronous App Server notifications.
type RunTracker struct {
	mu    sync.RWMutex
	runID string
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
	t.mu.Unlock()
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
	}
	t.mu.Unlock()
}

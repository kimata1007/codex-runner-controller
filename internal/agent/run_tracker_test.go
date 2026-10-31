package agent

import "testing"

func TestRunTrackerClearOnlyMatchesCurrentRun(t *testing.T) {
	tracker := NewRunTracker()
	tracker.Set("run-1")
	tracker.Clear("run-old")
	if tracker.Current() != "run-1" {
		t.Fatalf("current after stale clear = %q", tracker.Current())
	}
	tracker.Clear("run-1")
	if tracker.Current() != "" {
		t.Fatalf("current after clear = %q", tracker.Current())
	}
}

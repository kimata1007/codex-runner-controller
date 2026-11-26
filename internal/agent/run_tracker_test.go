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

func TestRunTrackerTurnIdentity(t *testing.T) {
	tracker := NewRunTracker()
	if !tracker.Begin("run-1") {
		t.Fatal("could not claim run")
	}
	tracker.RecordTurn("run-1", "thread-1", "turn-1")
	threadID, turnID, ok := tracker.Turn("run-1")
	if !ok || threadID != "thread-1" || turnID != "turn-1" {
		t.Fatal("active turn identity not recorded")
	}
	tracker.Clear("run-1")
	if !tracker.Begin("run-2") {
		t.Fatal("could not claim next run")
	}
	tracker.RecordTurn("run-1", "thread-1", "turn-1")
	if _, _, ok := tracker.Turn("run-2"); ok {
		t.Fatal("stale run must not identify a new turn")
	}
}

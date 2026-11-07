package events

import (
	"testing"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/runs"
)

func TestBrokerPublishesOnlyToMatchingRun(t *testing.T) {
	broker := NewBroker()
	first, cancelFirst := broker.Subscribe("run-1")
	defer cancelFirst()
	second, cancelSecond := broker.Subscribe("run-2")
	defer cancelSecond()

	broker.Publish(runs.Event{RunID: "run-1", Sequence: 1, EventType: "delta"})
	select {
	case event := <-first:
		if event.RunID != "run-1" || event.Sequence != 1 {
			t.Fatalf("event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
	select {
	case event := <-second:
		t.Fatalf("unexpected event = %+v", event)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestBrokerCancelClosesSubscription(t *testing.T) {
	broker := NewBroker()
	channel, cancel := broker.Subscribe("run-1")
	cancel()
	select {
	case _, ok := <-channel:
		if ok {
			t.Fatal("subscription channel is still open")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for closed channel")
	}
}

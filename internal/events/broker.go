package events

import (
	"sync"

	"github.com/kimata1007/codex-runner-controller/internal/runs"
)

const subscriberBuffer = 32

// Broker distributes live events. Durable recovery is handled by Store and is
// intentionally separate from this process-local notification mechanism.
type Broker struct {
	mu          sync.Mutex
	nextID      uint64
	subscribers map[string]map[uint64]chan runs.Event
}

func NewBroker() *Broker {
	return &Broker{subscribers: make(map[string]map[uint64]chan runs.Event)}
}

func (b *Broker) Subscribe(runID string) (<-chan runs.Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	id := b.nextID
	channel := make(chan runs.Event, subscriberBuffer)
	if b.subscribers[runID] == nil {
		b.subscribers[runID] = make(map[uint64]chan runs.Event)
	}
	b.subscribers[runID][id] = channel
	return channel, func() { b.unsubscribe(runID, id) }
}

func (b *Broker) Publish(event runs.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, channel := range b.subscribers[event.RunID] {
		select {
		case channel <- event:
		default:
			// The event is durable in Store. A slow subscriber can recover from
			// its last SSE id instead of blocking App Server event handling.
		}
	}
}

func (b *Broker) unsubscribe(runID string, id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if subscribers := b.subscribers[runID]; subscribers != nil {
		if channel, ok := subscribers[id]; ok {
			delete(subscribers, id)
			close(channel)
		}
		if len(subscribers) == 0 {
			delete(b.subscribers, runID)
		}
	}
}

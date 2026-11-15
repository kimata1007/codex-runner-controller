package agent

import (
	"context"
	"fmt"
	"time"
)

const runEventQueueCapacity = 256

// QueuedRunEventSender forwards App Server events in the order they are
// received while keeping network I/O off the JSON-RPC reader goroutine.
type QueuedRunEventSender struct {
	ctx     context.Context
	sender  RunEventSender
	queue   chan AppServerRunEvent
	onError func(error)
}

func NewQueuedRunEventSender(ctx context.Context, sender RunEventSender, onError func(error)) *QueuedRunEventSender {
	if ctx == nil {
		ctx = context.Background()
	}
	queued := &QueuedRunEventSender{
		ctx: ctx, sender: sender, queue: make(chan AppServerRunEvent, runEventQueueCapacity), onError: onError,
	}
	go queued.forward()
	return queued
}

func (s *QueuedRunEventSender) Send(ctx context.Context, event AppServerRunEvent) error {
	if s == nil || s.sender == nil {
		return fmt.Errorf("queued event sender: sender is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	event.Payload = append(event.Payload[:0:0], event.Payload...)
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	case s.queue <- event:
		return nil
	}
}

func (s *QueuedRunEventSender) forward() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case event := <-s.queue:
			var err error
			for attempt := 0; attempt < 3; attempt++ {
				ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
				err = s.sender.Send(ctx, event)
				cancel()
				if err == nil || s.ctx.Err() != nil {
					break
				}
				if attempt < 2 {
					select {
					case <-s.ctx.Done():
					case <-time.After(time.Duration(attempt+1) * 100 * time.Millisecond):
					}
				}
			}
			if err != nil && s.onError != nil {
				s.onError(err)
			}
		}
	}
}

var _ RunEventSender = (*QueuedRunEventSender)(nil)

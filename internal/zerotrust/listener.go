package zerotrust

import (
	"context"
	"log/slog"
	"net"
	"sync"
)

// ServeConnections bounds concurrent handshakes and streams. Closing the
// listener and canceling handlers are tied to the same shutdown context.
func ServeConnections(ctx context.Context, listener net.Listener, slots chan struct{}, handle func(context.Context, net.Conn) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()
	var handlers sync.WaitGroup
	defer func() {
		cancel()
		handlers.Wait()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case slots <- struct{}{}:
			handlers.Add(1)
			go func() {
				defer handlers.Done()
				defer func() { <-slots }()
				defer conn.Close()
				stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
				defer stop()
				if err := handle(ctx, conn); err != nil && ctx.Err() == nil {
					slog.DebugContext(ctx, "proxy connection closed", "operation", "transport.relay", "decision", "closed")
				}
			}()
		default:
			_ = conn.Close()
		}
	}
}

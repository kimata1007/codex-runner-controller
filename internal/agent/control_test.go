package agent

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/sessioncontrol"
)

type countedControlSession struct {
	fakeSession
	turns atomic.Int32
}

func (s *countedControlSession) StartTurn(context.Context, string, []TurnInput) error {
	s.turns.Add(1)
	return nil
}

func TestSessionControlReplaysUnacknowledgedEventsAndDeduplicatesCommands(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	tracker := NewRunTracker()
	control := NewSessionControl(ctx, tracker)
	session := &countedControlSession{}
	handler := control.Handler(session, nil)
	finished := make(chan struct{}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
		finished <- struct{}{}
	}))
	defer server.Close()

	dial := func() *sessioncontrol.Connection {
		t.Helper()
		transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
		}}
		conn, err := sessioncontrol.Dial(ctx, transport)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(conn.Close)
		return conn
	}
	read := func(conn *sessioncontrol.Connection, kind sessioncontrol.Kind) sessioncontrol.Frame {
		t.Helper()
		frame, err := conn.Receive()
		if err != nil || frame.Kind != kind || frame.ID != "run-1" || frame.Error != "" {
			t.Fatalf("received %+v, %v, expected %s", frame, err, kind)
		}
		return frame
	}
	send := func(conn *sessioncontrol.Connection, frame sessioncontrol.Frame) {
		t.Helper()
		if err := conn.Send(frame); err != nil {
			t.Fatal(err)
		}
	}
	turn := sessioncontrol.Frame{Kind: sessioncontrol.Command, ID: "run-1", Method: "turn/start",
		Payload: json.RawMessage(`{"threadId":"thread-1","text":"hello"}`)}
	first := dial()
	send(first, turn)
	read(first, sessioncontrol.Reply)
	if err := control.Send(ctx, AppServerRunEvent{RunID: "run-1", EventType: "turn/started", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	event := read(first, sessioncontrol.Event)
	first.Close()
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	second := dial()
	replay := read(second, sessioncontrol.Event)
	if event.Sequence != 1 || replay.Sequence != event.Sequence || replay.Method != event.Method {
		t.Fatalf("unexpected replay: %+v after %+v", replay, event)
	}
	send(second, sessioncontrol.Frame{Kind: sessioncontrol.Ack, ID: "run-1", Sequence: replay.Sequence})
	send(second, turn)
	read(second, sessioncontrol.Reply)
	if session.turns.Load() != 1 {
		t.Fatalf("executed %d turns after retry", session.turns.Load())
	}
	second.Close()
}

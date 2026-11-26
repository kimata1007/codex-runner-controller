package sessioncontrol

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFullDuplexStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Serve(w, r, func(_ context.Context, stream *Stream) {
			command, err := stream.Receive()
			if err != nil || command.Kind != Command || command.Method != "turn/start" {
				t.Errorf("server received %+v, %v", command, err)
				return
			}
			for _, frame := range []Frame{
				{Kind: Event, ID: command.ID, Sequence: 1, Method: "turn/started", Payload: json.RawMessage(`{}`)},
				{Kind: Reply, ID: command.ID},
			} {
				if err := stream.Send(frame); err != nil {
					t.Error(err)
					return
				}
			}
			ack, err := stream.Receive()
			if err != nil || ack.Kind != Ack || ack.ID != command.ID || ack.Sequence != 1 {
				t.Errorf("server received %+v, %v", ack, err)
			}
		})
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}}
	defer transport.CloseIdleConnections()
	conn, err := Dial(ctx, transport)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.Send(Frame{Kind: Command, ID: "turn-1", Method: "turn/start"}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []Kind{Event, Reply} {
		frame, err := conn.Receive()
		if err != nil || frame.Kind != kind || frame.ID != "turn-1" {
			t.Fatalf("client received %+v, %v, expected %q", frame, err, kind)
		}
	}
	if err := conn.Send(Frame{Kind: Ack, ID: "turn-1", Sequence: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Receive(); err != io.EOF {
		t.Fatalf("stream did not finish: %v", err)
	}
}

func TestRejectInvalidFrames(t *testing.T) {
	for _, data := range []string{
		`{"kind":"heartbeat","runId":"untrusted"}` + "\n",
		`{"kind":"event","id":"run","method":"turn/started","payload":{}}` + "\n",
		`{"kind":"heartbeat"}`,
		strings.Repeat(" ", MaxFrameBytes+1) + "\n",
	} {
		stream := newStream(strings.NewReader(data), nil)
		if _, err := stream.Receive(); err == nil {
			t.Fatal("received an invalid control frame")
		}
	}
}

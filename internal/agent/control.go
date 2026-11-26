package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/sessioncontrol"
)

const (
	maxPendingEvents = 256
	maxPendingBytes  = 8 << 20
	maxCommandCache  = 128
	commandTimeout   = 60 * time.Second
)

type SessionControl struct {
	lifetime context.Context
	tracker  *RunTracker
	attached atomic.Bool
	wakeup   chan struct{}

	mu        sync.Mutex
	runID     string
	nextSeq   uint64
	offered   uint64
	failed    bool
	events    []sessioncontrol.Frame
	bytes     int
	commands  map[string]*controlCommand
	completed []string
}

type controlCommand struct {
	method string
	digest [sha256.Size]byte
	reply  sessioncontrol.Frame
	done   chan struct{}
}

func NewSessionControl(ctx context.Context, tracker *RunTracker) *SessionControl {
	return &SessionControl{lifetime: ctx, tracker: tracker, wakeup: make(chan struct{}, 1), commands: make(map[string]*controlCommand)}
}

func (s *SessionControl) Send(_ context.Context, event AppServerRunEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if event.RunID == "" || event.RunID != s.runID || s.failed {
		return errors.New("session control event has no active binding")
	}
	payload := event.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	method := event.EventType
	if method == "" || len(method) > 128 || !json.Valid(payload) || len(payload) > sessioncontrol.MaxFrameBytes-1024 ||
		len(s.events) >= maxPendingEvents || s.bytes+len(payload) > maxPendingBytes {
		// Reserve room for a terminal failure after the already queued events.
		// The Runner can acknowledge the failure without assuming a missing event.
		method = "turn/failed"
		payload = json.RawMessage(`{"error":{"message":"Session event exceeded the control stream limit"}}`)
		s.failed = true
	}
	s.nextSeq++
	s.bytes += len(payload)
	s.events = append(s.events, sessioncontrol.Frame{Kind: sessioncontrol.Event, ID: event.RunID,
		Sequence: s.nextSeq, Method: method, Payload: append(json.RawMessage(nil), payload...)})
	select {
	case s.wakeup <- struct{}{}:
	default:
	}
	return nil
}

func (s *SessionControl) Handler(session SessionAPI, approvals *ApprovalManager) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.attached.CompareAndSwap(false, true) {
			http.Error(w, "session already has a control connection", http.StatusConflict)
			return
		}
		defer s.attached.Store(false)
		sessioncontrol.Serve(w, r, func(ctx context.Context, stream *sessioncontrol.Stream) {
			s.exchange(ctx, stream, session, approvals)
		})
	})
}

func (s *SessionControl) exchange(ctx context.Context, stream *sessioncontrol.Stream, session SessionAPI, approvals *ApprovalManager) {
	ctx, cancel := context.WithCancel(ctx)
	readerDone := make(chan struct{})
	defer func() {
		cancel()
		stream.StopReceiving()
		<-readerDone
	}()
	replies := make(chan sessioncontrol.Frame, 8)
	slots := make(chan struct{}, cap(replies))
	go func() {
		defer close(readerDone)
		defer cancel()
		for {
			frame, err := stream.Receive()
			if err != nil {
				return
			}
			switch frame.Kind {
			case sessioncontrol.Command:
				select {
				case slots <- struct{}{}:
				default:
					return
				}
				go func() {
					defer func() { <-slots }()
					reply := s.command(ctx, session, approvals, frame)
					select {
					case replies <- reply:
					case <-ctx.Done():
					}
				}()
			case sessioncontrol.Ack:
				if !s.ack(frame) {
					return
				}
			case sessioncontrol.Heartbeat:
			default:
				return
			}
		}
	}()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	var lastRun string
	var lastSent uint64
	ready := make(chan struct{})
	close(ready)
	for ctx.Err() == nil {
		event, ok := s.pending(lastRun, lastSent)
		var pending <-chan struct{}
		if ok {
			pending = ready
		}
		select {
		case <-ctx.Done():
			return
		case reply := <-replies:
			if err := stream.Send(reply); err != nil {
				return
			}
		case <-ticker.C:
			if err := stream.Send(sessioncontrol.Frame{Kind: sessioncontrol.Heartbeat}); err != nil {
				return
			}
		case <-s.wakeup:
		case <-pending:
			if err := stream.Send(event); err != nil {
				return
			}
			lastRun, lastSent = event.ID, event.Sequence
		}
	}
}

func (s *SessionControl) pending(runID string, after uint64) (sessioncontrol.Frame, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if runID != s.runID {
		after = 0
	}
	for _, event := range s.events {
		if event.Sequence > after {
			s.offered = max(s.offered, event.Sequence)
			return event, true
		}
	}
	return sessioncontrol.Frame{}, false
}

func (s *SessionControl) ack(ack sessioncontrol.Frame) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ack.ID != s.runID || ack.Sequence > s.offered {
		return false
	}
	count := 0
	for count < len(s.events) && s.events[count].Sequence <= ack.Sequence {
		s.bytes -= len(s.events[count].Payload)
		count++
	}
	clear(s.events[:count])
	s.events = s.events[count:]
	return true
}

func (s *SessionControl) command(ctx context.Context, session SessionAPI, approvals *ApprovalManager, frame sessioncontrol.Frame) sessioncontrol.Frame {
	reply := sessioncontrol.Frame{Kind: sessioncontrol.Reply, ID: frame.ID}
	digest := sha256.Sum256(frame.Payload)
	s.mu.Lock()
	entry := s.commands[frame.ID]
	if entry == nil {
		if len(s.commands) >= maxCommandCache {
			if len(s.completed) == 0 {
				s.mu.Unlock()
				reply.Error = "session command capacity exceeded"
				return reply
			}
			delete(s.commands, s.completed[0])
			s.completed = s.completed[1:]
		}
		entry = &controlCommand{method: frame.Method, digest: digest, done: make(chan struct{})}
		s.commands[frame.ID] = entry
	} else if entry.method != frame.Method || entry.digest != digest {
		s.mu.Unlock()
		reply.Error = "command ID already has a different request"
		return reply
	} else {
		s.mu.Unlock()
		select {
		case <-entry.done:
			return entry.reply
		case <-ctx.Done():
			reply.Error = "session connection closed"
			return reply
		}
	}
	s.mu.Unlock()

	commandCtx, cancel := context.WithTimeout(s.lifetime, commandTimeout)
	defer cancel()
	payload, err := s.execute(commandCtx, session, approvals, frame)
	if err != nil {
		reply.Error = err.Error()
	} else {
		reply.Payload = payload
	}
	s.mu.Lock()
	entry.reply = reply
	s.completed = append(s.completed, frame.ID)
	close(entry.done)
	s.mu.Unlock()
	return reply
}

func (s *SessionControl) execute(ctx context.Context, session SessionAPI, approvals *ApprovalManager, command sessioncontrol.Frame) (json.RawMessage, error) {
	var params struct {
		ThreadStartParams
		ThreadID  string          `json:"threadId"`
		TurnID    string          `json:"turnId"`
		RunID     string          `json:"runId"`
		Text      string          `json:"text"`
		RequestID string          `json:"requestId"`
		Response  json.RawMessage `json:"response"`
	}
	if len(command.Payload) > 0 && json.Unmarshal(command.Payload, &params) != nil {
		return nil, errors.New("invalid command parameters")
	}
	switch command.Method {
	case "session/status":
		return json.Marshal(struct {
			Active bool `json:"active"`
		}{s.tracker.Current() != ""})
	case "thread/start", "thread/resume":
		var thread Thread
		var err error
		if command.Method == "thread/start" {
			thread, err = session.StartThread(ctx, params.ThreadStartParams)
		} else {
			thread, err = session.ResumeThread(ctx, params.ThreadID, params.ThreadStartParams)
		}
		if err != nil || len(thread.ID) > 128 {
			return nil, errors.New("Codex App Server rejected the thread request")
		}
		return json.Marshal(thread)
	case "turn/start":
		if params.ThreadID == "" || strings.TrimSpace(params.Text) == "" || !s.begin(command.ID) {
			return nil, errors.New("cannot start turn with the supplied session state")
		}
		if err := session.StartTurn(ctx, params.ThreadID, []TurnInput{{Type: "text", Text: params.Text}}); err != nil {
			s.tracker.Clear(command.ID)
			return nil, errors.New("Codex App Server rejected the turn request")
		}
	case "turn/interrupt":
		interrupt, ok := session.(interface {
			InterruptTurn(context.Context, string, string) error
		})
		if !ok || !s.active(params.RunID) || params.ThreadID == "" || params.TurnID == "" ||
			interrupt.InterruptTurn(ctx, params.ThreadID, params.TurnID) != nil {
			return nil, errors.New("could not interrupt the active turn")
		}
	case "approval/respond":
		if approvals == nil || !s.active(params.RunID) || params.RequestID == "" || len(params.Response) == 0 ||
			approvals.ResolveForRun(params.RunID, params.RequestID, params.Response) != nil {
			return nil, errors.New("approval request is unavailable for the active run")
		}
	default:
		return nil, errors.New("unsupported session command")
	}
	return nil, nil
}

func (s *SessionControl) active(runID string) bool {
	return runID != "" && s.tracker.Current() == runID
}

func (s *SessionControl) begin(runID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.events) != 0 || s.runID == runID || !s.tracker.Begin(runID) {
		return false
	}
	s.runID, s.nextSeq, s.offered, s.failed = runID, 0, 0, false
	return true
}

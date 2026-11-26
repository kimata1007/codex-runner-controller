package agentclient

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/agent"
	"github.com/kimata1007/codex-runner-controller/internal/runs"
	"github.com/kimata1007/codex-runner-controller/internal/sessioncontrol"
	"github.com/kimata1007/codex-runner-controller/internal/zerotrust"
	"k8s.io/apimachinery/pkg/types"
)

type ControlTarget struct {
	PodUID types.UID
	Params agent.ThreadStartParams
}

type ControlTargetResolver interface {
	ResolveControl(context.Context, string) (ControlTarget, error)
}

type ControlAuthority interface {
	BindControlRun(context.Context, string, string, string) error
	ControlEvent(context.Context, string, string, sessioncontrol.Frame) (bool, error)
	ControlHeartbeat(context.Context, string)
}

type ControlFactory struct {
	ctx       context.Context
	resolver  ControlTargetResolver
	authority ControlAuthority
	socket    string
	mu        sync.Mutex
	links     map[string]*controlLink
}

func NewControlFactory(ctx context.Context, resolver ControlTargetResolver, authority ControlAuthority, socket string) *ControlFactory {
	return &ControlFactory{ctx: ctx, resolver: resolver, authority: authority, socket: socket, links: make(map[string]*controlLink)}
}

func (f *ControlFactory) CreateClientForConversation(ctx context.Context, conversationID string) (runs.ThreadClient, error) {
	target, err := f.resolver.ResolveControl(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	return &controlClient{link: f.link(conversationID, target.PodUID, false), params: target.Params}, nil
}

func (f *ControlFactory) Restore(conversationID string, podUID types.UID) {
	f.link(conversationID, podUID, true)
}

func (f *ControlFactory) link(conversationID string, podUID types.UID, active bool) *controlLink {
	f.mu.Lock()
	defer f.mu.Unlock()
	if link := f.links[conversationID]; link != nil {
		if link.podUID == podUID && link.ctx.Err() == nil {
			return link
		}
		link.cancel()
	}
	ctx, cancel := context.WithCancel(f.ctx)
	link := &controlLink{ctx: ctx, cancel: cancel, authority: f.authority, podUID: podUID, conversationID: conversationID,
		changed: make(chan struct{}), pending: make(map[string]chan sessioncontrol.Frame), lastUsed: time.Now(), active: active}
	f.links[conversationID] = link
	transport := &http.Transport{ResponseHeaderTimeout: 8 * time.Second,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return zerotrust.DialSessionControl(ctx, f.socket, podUID)
		}}
	go func() {
		defer func() {
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.links[conversationID] == link {
				delete(f.links, conversationID)
			}
		}()
		link.run(transport)
	}()
	return link
}

type controlLink struct {
	ctx            context.Context
	cancel         context.CancelFunc
	authority      ControlAuthority
	podUID         types.UID
	conversationID string
	eventGate      sync.Mutex

	mu       sync.Mutex
	conn     *sessioncontrol.Connection
	changed  chan struct{}
	pending  map[string]chan sessioncontrol.Frame
	lastUsed time.Time
	active   bool
}

func (l *controlLink) setConnection(conn *sessioncontrol.Connection) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.conn = conn
	close(l.changed)
	l.changed = make(chan struct{})
}

func (l *controlLink) run(transport *http.Transport) {
	defer l.cancel()
	defer transport.CloseIdleConnections()
	go l.expireIdle()
	for l.ctx.Err() == nil {
		conn, err := sessioncontrol.Dial(l.ctx, transport)
		if err == nil {
			l.setConnection(conn)
			l.receive(conn)
			l.setConnection(nil)
			conn.Close()
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-l.ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (l *controlLink) expireIdle() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-l.ctx.Done():
			return
		case <-ticker.C:
			l.mu.Lock()
			if !l.active && len(l.pending) == 0 && time.Since(l.lastUsed) > time.Minute {
				l.cancel()
			}
			l.mu.Unlock()
		}
	}
}

func (l *controlLink) receive(conn *sessioncontrol.Connection) {
	timeout := time.AfterFunc(45*time.Second, conn.Close)
	defer timeout.Stop()
	for {
		frame, err := conn.Receive()
		if err != nil {
			return
		}
		timeout.Reset(45 * time.Second)
		switch frame.Kind {
		case sessioncontrol.Reply:
			l.mu.Lock()
			if waiter := l.pending[frame.ID]; waiter != nil {
				select {
				case waiter <- frame:
				default:
				}
			}
			l.mu.Unlock()
		case sessioncontrol.Event:
			if err := l.persist(conn, frame); err != nil {
				return
			}
		case sessioncontrol.Heartbeat:
			l.mu.Lock()
			active := l.active
			l.mu.Unlock()
			if active {
				ctx, cancel := context.WithTimeout(l.ctx, 5*time.Second)
				l.authority.ControlHeartbeat(ctx, l.conversationID)
				cancel()
			}
		default:
			return
		}
	}
}

func (l *controlLink) persist(conn *sessioncontrol.Connection, frame sessioncontrol.Frame) error {
	l.eventGate.Lock()
	defer l.eventGate.Unlock()
	ctx, cancel := context.WithTimeout(l.ctx, 10*time.Second)
	defer cancel()
	terminal, err := l.authority.ControlEvent(ctx, l.conversationID, string(l.podUID), frame)
	if err != nil {
		return err
	}
	if terminal {
		l.mu.Lock()
		l.active = false
		l.lastUsed = time.Now()
		l.mu.Unlock()
	}
	return conn.Send(sessioncontrol.Frame{Kind: sessioncontrol.Ack, ID: frame.ID, Sequence: frame.Sequence})
}

func (l *controlLink) call(ctx context.Context, frame sessioncontrol.Frame) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	replies := make(chan sessioncontrol.Frame, 1)
	l.mu.Lock()
	if _, exists := l.pending[frame.ID]; exists || len(l.pending) >= 8 {
		l.mu.Unlock()
		return nil, errors.New("session command is already pending or capacity is exhausted")
	}
	l.pending[frame.ID] = replies
	l.lastUsed = time.Now()
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		delete(l.pending, frame.ID)
		l.lastUsed = time.Now()
		l.mu.Unlock()
	}()
	for {
		l.mu.Lock()
		conn, changed := l.conn, l.changed
		l.mu.Unlock()
		if conn != nil {
			l.eventGate.Lock()
			stop := context.AfterFunc(ctx, conn.Close)
			err := conn.Send(frame)
			stop()
			l.eventGate.Unlock()
			if err != nil {
				conn.Close()
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-l.ctx.Done():
			return nil, l.ctx.Err()
		case <-changed:
		case reply := <-replies:
			if reply.Error != "" {
				return nil, errors.New(reply.Error)
			}
			return reply.Payload, nil
		}
	}
}

type controlClient struct {
	link   *controlLink
	params agent.ThreadStartParams
}

func (c *controlClient) StartThread(ctx context.Context) (string, error) {
	response, err := c.link.call(ctx, newControlCommand("thread/start", c.params))
	if err != nil {
		return "", err
	}
	var thread agent.Thread
	if json.Unmarshal(response, &thread) != nil || thread.ID == "" || len(thread.ID) > 128 {
		return "", errors.New("invalid session thread response")
	}
	return thread.ID, nil
}

func (c *controlClient) ResumeThread(ctx context.Context, threadID string) error {
	request := struct {
		agent.ThreadStartParams
		ThreadID string `json:"threadId"`
	}{c.params, threadID}
	_, err := c.link.call(ctx, newControlCommand("thread/resume", request))
	return err
}

func (c *controlClient) StartTurn(ctx context.Context, threadID, runID, text string) error {
	if err := c.link.authority.BindControlRun(ctx, c.link.conversationID, runID, string(c.link.podUID)); err != nil {
		return err
	}
	c.link.mu.Lock()
	c.link.active = true
	c.link.mu.Unlock()
	payload, err := json.Marshal(map[string]string{"threadId": threadID, "text": text})
	if err != nil {
		return err
	}
	_, err = c.link.call(ctx, sessioncontrol.Frame{Kind: sessioncontrol.Command, ID: runID, Method: "turn/start", Payload: payload})
	if err != nil {
		c.link.mu.Lock()
		c.link.active = false
		c.link.mu.Unlock()
	}
	return err
}

func (c *controlClient) RespondRunApproval(ctx context.Context, runID, requestID string, response any) error {
	payload, err := json.Marshal(map[string]any{"runId": runID, "requestId": requestID, "response": response})
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(runID + "\x00" + requestID))
	_, err = c.link.call(ctx, sessioncontrol.Frame{Kind: sessioncontrol.Command, ID: hex.EncodeToString(sum[:]), Method: "approval/respond", Payload: payload})
	return err
}

func newControlCommand(method string, params any) sessioncontrol.Frame {
	var id [16]byte
	_, _ = rand.Read(id[:])
	payload, _ := json.Marshal(params)
	return sessioncontrol.Frame{Kind: sessioncontrol.Command, ID: hex.EncodeToString(id[:]), Method: method, Payload: payload}
}

var _ runs.ThreadClientFactory = (*ControlFactory)(nil)

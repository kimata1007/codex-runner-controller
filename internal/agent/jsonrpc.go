package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

type NotificationHandler func(AppServerNotification)
type RequestHandler func(context.Context, Message) (any, error)

func ChainRequestHandlers(handlers ...RequestHandler) RequestHandler {
	return func(ctx context.Context, message Message) (any, error) {
		for _, handler := range handlers {
			if handler == nil {
				continue
			}
			result, err := handler(ctx, message)
			if err == nil {
				return result, nil
			}
			if err != errRequestNotHandled {
				return nil, err
			}
		}
		return nil, errRequestNotHandled
	}
}

var errRequestNotHandled = fmt.Errorf("request not handled")

type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type JSONRPCClient struct {
	reader         *bufio.Reader
	writer         io.Writer
	handler        NotificationHandler
	requestHandler RequestHandler

	callMu     sync.Mutex
	writeMu    sync.Mutex
	pendingMu  sync.Mutex
	nextID     int64
	pending    map[int64]chan callResult
	readErr    error
	readerOnce sync.Once
}

func (c *JSONRPCClient) SetRequestHandler(handler RequestHandler) {
	if c != nil {
		c.requestHandler = handler
	}
}

type callResult struct {
	message Message
	err     error
}

func NewJSONRPCClient(reader io.Reader, writer io.Writer, handler NotificationHandler) *JSONRPCClient {
	return &JSONRPCClient{
		reader:  bufio.NewReader(reader),
		writer:  writer,
		handler: handler,
		pending: make(map[int64]chan callResult),
	}
}

func (c *JSONRPCClient) Call(ctx context.Context, method string, params any, result any) error {
	c.callMu.Lock()
	defer c.callMu.Unlock()

	id := c.nextRequestID()
	responseCh := make(chan callResult, 1)
	c.pendingMu.Lock()
	if c.readErr != nil {
		err := c.readErr
		c.pendingMu.Unlock()
		return err
	}
	c.pending[id] = responseCh
	c.pendingMu.Unlock()
	c.readerOnce.Do(func() { go c.readLoop() })

	if err := c.write(Message{JSONRPC: "2.0", ID: &id, Method: method, Params: mustRawJSON(params)}); err != nil {
		c.removePending(id)
		return err
	}

	var response callResult
	select {
	case <-ctx.Done():
		c.removePending(id)
		return ctx.Err()
	case response = <-responseCh:
	}
	if response.err != nil {
		return response.err
	}
	if response.message.Error != nil {
		return fmt.Errorf("jsonrpc %d: %s", response.message.Error.Code, response.message.Error.Message)
	}
	if result == nil || len(response.message.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(response.message.Result, result); err != nil {
		return fmt.Errorf("jsonrpc: decode %s result: %w", method, err)
	}
	return nil
}

func (c *JSONRPCClient) Notify(method string, params any) error {
	return c.write(Message{JSONRPC: "2.0", Method: method, Params: mustRawJSON(params)})
}

func (c *JSONRPCClient) nextRequestID() int64 {
	c.nextID++
	return c.nextID
}

func (c *JSONRPCClient) write(message Message) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("jsonrpc: encode message: %w", err)
	}
	payload = append(payload, '\n')
	if _, err := c.writer.Write(payload); err != nil {
		return fmt.Errorf("jsonrpc: write message: %w", err)
	}
	return nil
}

// readLoop runs for the lifetime of the App Server connection. JSON-RPC
// responses complete a waiting Call; server notifications are delivered
// separately to the notification handler, including those arriving after a
// Call returns.
func (c *JSONRPCClient) readLoop() {
	for {
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			c.failPending(fmt.Errorf("jsonrpc: read message: %w", err))
			return
		}
		var message Message
		if err := json.Unmarshal(line, &message); err != nil {
			c.failPending(fmt.Errorf("jsonrpc: decode message: %w", err))
			return
		}
		if message.ID == nil {
			if c.handler != nil && message.Method != "" {
				c.handler(AppServerNotification{Method: message.Method, Params: message.Params})
			}
			continue
		}
		if message.Method != "" {
			c.handleRequest(context.Background(), message)
			continue
		}
		c.pendingMu.Lock()
		responseCh := c.pending[*message.ID]
		delete(c.pending, *message.ID)
		c.pendingMu.Unlock()
		if responseCh != nil {
			responseCh <- callResult{message: message}
		}
	}
}

func (c *JSONRPCClient) handleRequest(ctx context.Context, message Message) {
	response := Message{JSONRPC: "2.0", ID: message.ID}
	if c.requestHandler == nil {
		response.Error = &RPCError{Code: -32601, Message: "method not handled"}
		_ = c.write(response)
		return
	}
	result, err := c.requestHandler(ctx, message)
	if err != nil {
		if err == errRequestNotHandled {
			response.Error = &RPCError{Code: -32601, Message: "method not handled"}
		} else {
			response.Error = &RPCError{Code: -32000, Message: err.Error()}
		}
	} else {
		response.Result = mustRawJSON(result)
	}
	_ = c.write(response)
}

func (c *JSONRPCClient) removePending(id int64) {
	c.pendingMu.Lock()
	delete(c.pending, id)
	c.pendingMu.Unlock()
}

func (c *JSONRPCClient) failPending(err error) {
	c.pendingMu.Lock()
	c.readErr = err
	pending := c.pending
	c.pending = make(map[int64]chan callResult)
	c.pendingMu.Unlock()
	for _, responseCh := range pending {
		responseCh <- callResult{err: err}
	}
}

func mustRawJSON(value any) json.RawMessage {
	if value == nil {
		return json.RawMessage(`{}`)
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(strconv.Quote(err.Error()))
	}
	return payload
}

type Process struct {
	Command *exec.Cmd
	Client  *JSONRPCClient
	waitErr chan error
}

func StartProcess(ctx context.Context, command string, args []string, handler NotificationHandler) (*Process, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = appServerEnvironment(os.Environ())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("app-server stdout: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("app-server stdin: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start app-server: %w", err)
	}
	process := &Process{Command: cmd, waitErr: make(chan error, 1)}
	process.Client = NewJSONRPCClient(stdout, stdin, handler)
	go func() { process.waitErr <- cmd.Wait() }()
	return process, nil
}

// appServerEnvironment keeps control-plane credentials in runner-agent. The
// App Server can execute commands on behalf of a conversation, so it must not
// inherit service-to-service tokens or the Git clone token.
func appServerEnvironment(environment []string) []string {
	blocked := map[string]struct{}{
		"GIT_AUTH_TOKEN":     {},
		"RUNNER_AGENT_TOKEN": {},
		"RUNNER_API_TOKEN":   {},
	}
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			filtered = append(filtered, entry)
			continue
		}
		if _, secret := blocked[name]; !secret {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func (p *Process) Wait() error { return <-p.waitErr }

func (p *Process) Close() error {
	if p == nil || p.Command == nil || p.Command.Process == nil {
		return nil
	}
	return p.Command.Process.Kill()
}

package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

type EventHandler func(Message)

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
	reader  *bufio.Reader
	writer  io.Writer
	handler EventHandler

	mu     sync.Mutex
	nextID int64
}

func NewJSONRPCClient(reader io.Reader, writer io.Writer, handler EventHandler) *JSONRPCClient {
	return &JSONRPCClient{reader: bufio.NewReader(reader), writer: writer, handler: handler}
}

func (c *JSONRPCClient) Call(ctx context.Context, method string, params any, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextRequestID()
	if err := c.write(Message{JSONRPC: "2.0", ID: &id, Method: method, Params: mustRawJSON(params)}); err != nil {
		return err
	}
	for {
		line, err := c.readLine(ctx)
		if err != nil {
			return err
		}
		var message Message
		if err := json.Unmarshal(line, &message); err != nil {
			return fmt.Errorf("jsonrpc: decode message: %w", err)
		}
		if message.ID == nil || *message.ID != id {
			c.handleEvent(message)
			continue
		}
		if message.Error != nil {
			return fmt.Errorf("jsonrpc %d: %s", message.Error.Code, message.Error.Message)
		}
		if result == nil || len(message.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(message.Result, result); err != nil {
			return fmt.Errorf("jsonrpc: decode %s result: %w", method, err)
		}
		return nil
	}
}

func (c *JSONRPCClient) Notify(method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.write(Message{JSONRPC: "2.0", Method: method, Params: mustRawJSON(params)})
}

func (c *JSONRPCClient) nextRequestID() int64 {
	c.nextID++
	return c.nextID
}

func (c *JSONRPCClient) write(message Message) error {
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

func (c *JSONRPCClient) readLine(ctx context.Context) ([]byte, error) {
	lineCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			errCh <- err
			return
		}
		lineCh <- line
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errCh:
		return nil, fmt.Errorf("jsonrpc: read message: %w", err)
	case line := <-lineCh:
		return []byte(strings.TrimSpace(string(line))), nil
	}
}

func (c *JSONRPCClient) handleEvent(message Message) {
	if c.handler != nil && message.Method != "" {
		c.handler(message)
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

func StartProcess(ctx context.Context, command string, args []string, handler EventHandler) (*Process, error) {
	cmd := exec.CommandContext(ctx, command, args...)
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

func (p *Process) Wait() error { return <-p.waitErr }

func (p *Process) Close() error {
	if p == nil || p.Command == nil || p.Command.Process == nil {
		return nil
	}
	return p.Command.Process.Kill()
}

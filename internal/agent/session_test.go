package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestSessionInitializesAndStartsThread(t *testing.T) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()
	client := NewJSONRPCClient(clientReader, clientWriter, nil)
	session := NewSession(client)

	done := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(serverReader)
		for i := 0; i < 2; i++ {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				done <- err
				return
			}
			var message Message
			if err := json.Unmarshal(line, &message); err != nil {
				done <- err
				return
			}
			if message.Method == "initialize" {
				if err := writeMessage(serverWriter, Message{JSONRPC: "2.0", ID: message.ID, Result: json.RawMessage(`{"capabilities":{}}`)}); err != nil {
					done <- err
					return
				}
				continue
			}
			if message.Method == "initialized" {
				continue
			}
			done <- fmt.Errorf("unexpected method %q", message.Method)
			return
		}

		line, err := reader.ReadBytes('\n')
		if err != nil {
			done <- err
			return
		}
		var message Message
		if err := json.Unmarshal(line, &message); err != nil {
			done <- err
			return
		}
		if message.Method != "thread/start" {
			done <- fmt.Errorf("method = %q", message.Method)
			return
		}
		done <- writeMessage(serverWriter, Message{JSONRPC: "2.0", ID: message.ID, Result: json.RawMessage(`{"thread":{"id":"thread-1"}}`)})
	}()

	if err := session.Initialize(context.Background(), ClientInfo{Name: "test"}); err != nil {
		t.Fatal(err)
	}
	thread, err := session.StartThread(context.Background(), ThreadStartParams{Model: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if thread.ID != "thread-1" {
		t.Fatalf("thread id = %q", thread.ID)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSessionValidatesLifecycleInputs(t *testing.T) {
	client := NewJSONRPCClient(strings.NewReader(""), io.Discard, nil)
	session := NewSession(client)
	if _, err := session.ResumeThread(context.Background(), "", ThreadStartParams{}); err == nil {
		t.Fatal("expected missing thread id error")
	}
	if err := session.StartTurn(context.Background(), "thread-1", nil); err == nil {
		t.Fatal("expected missing input error")
	}
}

func writeMessage(w io.Writer, message Message) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", payload)
	return err
}

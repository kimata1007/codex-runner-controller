package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestJSONRPCClientCallHandlesEventsBeforeResponse(t *testing.T) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()
	var notifications []AppServerNotification
	client := NewJSONRPCClient(clientReader, clientWriter, func(notification AppServerNotification) { notifications = append(notifications, notification) })
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		request, err := bufio.NewReader(serverReader).ReadBytes('\n')
		if err != nil {
			return
		}
		var message Message
		_ = json.Unmarshal(request, &message)
		payload := []string{
			`{"jsonrpc":"2.0","method":"item/agentMessage/delta","params":{"delta":"hello"}}`,
			`{"jsonrpc":"2.0","id":1,"result":{"thread":{"id":"thread-1"}}}`,
		}
		_, _ = io.WriteString(serverWriter, strings.Join(payload, "\n")+"\n")
	}()

	var result struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := client.Call(context.Background(), "thread/start", map[string]string{"model": "default"}, &result); err != nil {
		t.Fatalf("call: %v", err)
	}
	<-serverDone
	if result.Thread.ID != "thread-1" || len(notifications) != 1 || notifications[0].Method != "item/agentMessage/delta" {
		t.Fatalf("result=%+v notifications=%v", result, notifications)
	}
}

func TestJSONRPCClientReturnsRemoteError(t *testing.T) {
	reader := strings.NewReader(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"denied"}}
`)
	var output strings.Builder
	client := NewJSONRPCClient(reader, &output, nil)
	if err := client.Call(context.Background(), "initialize", nil, nil); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("error = %v", err)
	}
}

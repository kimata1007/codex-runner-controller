package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestAppServerEnvironmentDoesNotExposeControlPlaneTokens(t *testing.T) {
	input := []string{
		"PATH=/usr/bin",
		"CODEX_HOME=/var/lib/codex",
		"ACCESS_TOKEN=chatgpt-access-token",
		"GIT_AUTH_TOKEN=git-token",
		"RUNNER_AGENT_TOKEN=agent-token",
		"RUNNER_API_TOKEN=api-token",
	}
	got := appServerEnvironment(input)
	want := []string{"PATH=/usr/bin", "CODEX_HOME=/var/lib/codex", "ACCESS_TOKEN=chatgpt-access-token"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("appServerEnvironment() = %v, want %v", got, want)
	}
}

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

func TestJSONRPCClientRespondsToServerRequest(t *testing.T) {
	reader := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":7,\"method\":\"approval/request\",\"params\":{}}\n{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n")
	var output strings.Builder
	client := NewJSONRPCClient(reader, &output, nil)
	client.SetRequestHandler(func(_ context.Context, message Message) (any, error) {
		if message.Method != "approval/request" {
			t.Fatalf("method = %q", message.Method)
		}
		return map[string]string{"decision": "decline"}, nil
	})
	if err := client.Call(context.Background(), "turn/start", nil, nil); err != nil {
		t.Fatalf("call: %v", err)
	}
	if !strings.Contains(output.String(), `"id":7`) || !strings.Contains(output.String(), `"decision":"decline"`) {
		t.Fatalf("response = %s", output.String())
	}
}

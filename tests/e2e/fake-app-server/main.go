// Minimal JSON-RPC/stdio Codex App Server test double.
// It runs as a child of the REAL runner-agent binary in Kind. No API key,
// network connection, or actual Codex model execution is needed.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
}

func send(encoder *json.Encoder, message any) {
	if err := encoder.Encode(message); err != nil {
		fmt.Fprintln(os.Stderr, "e2e app-server: encode failed")
		os.Exit(1)
	}
}

func main() {
	reader := bufio.NewScanner(os.Stdin)
	reader.Buffer(make([]byte, 4096), 1<<20)
	writer := json.NewEncoder(os.Stdout)
	waiting, approved := false, false
	for reader.Scan() {
		var incoming request
		if err := json.Unmarshal(reader.Bytes(), &incoming); err != nil {
			fmt.Fprintln(os.Stderr, "e2e app-server: invalid JSON-RPC request")
			os.Exit(1)
		}
		if incoming.ID == nil {
			continue // e.g. initialized notification
		}
		if waiting && incoming.Method == "" && *incoming.ID == 9001 {
			var result struct {
				Decision string `json:"decision"`
			}
			approved = json.Unmarshal(incoming.Result, &result) == nil && result.Decision == "accept"
			if approved {
				send(writer, map[string]any{"jsonrpc": "2.0", "method": "item/agentMessage/delta", "params": map[string]string{"delta": "e2e-approval-received"}})
			}
			continue
		}

		var result any
		switch incoming.Method {
		case "initialize":
			result = map[string]any{"capabilities": map[string]any{}}
		case "thread/start", "thread/resume":
			result = map[string]any{"thread": map[string]string{"id": "e2e-thread"}}
		case "turn/start":
			var params struct {
				ThreadID string `json:"threadId"`
				Input    []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"input"`
			}
			if err := json.Unmarshal(incoming.Params, &params); err != nil || params.ThreadID != "e2e-thread" || len(params.Input) != 1 ||
				params.Input[0].Type != "text" || (params.Input[0].Text != "e2e-message-from-api" && params.Input[0].Text != "e2e-hold-for-interrupt") {
				send(writer, map[string]any{"jsonrpc": "2.0", "id": incoming.ID, "error": map[string]any{"code": -32602, "message": "unexpected E2E turn input"}})
				continue
			}
			// This marker lets the E2E distinguish a real JSON-RPC child-process
			// call from an Agent HTTP stub that never launched a child process.
			fmt.Fprintln(os.Stderr, "E2E_CODEX_APP_SERVER_TURN_RECEIVED")
			waiting, approved = params.Input[0].Text == "e2e-hold-for-interrupt", false
			result = map[string]any{"turn": map[string]string{"id": "e2e-turn"}}
		case "turn/interrupt":
			var params struct {
				ThreadID string `json:"threadId"`
				TurnID   string `json:"turnId"`
			}
			if !waiting || !approved || json.Unmarshal(incoming.Params, &params) != nil || params.ThreadID != "e2e-thread" || params.TurnID != "e2e-turn" {
				send(writer, map[string]any{"jsonrpc": "2.0", "id": incoming.ID, "error": map[string]any{"code": -32602, "message": "unexpected interrupt"}})
				continue
			}
			waiting = false
			result = map[string]any{}
		default:
			send(writer, map[string]any{"jsonrpc": "2.0", "id": incoming.ID, "error": map[string]any{"code": -32601, "message": "unsupported E2E method: " + strings.TrimSpace(incoming.Method)}})
			continue
		}
		send(writer, map[string]any{"jsonrpc": "2.0", "id": incoming.ID, "result": result})

		switch {
		case incoming.Method == "turn/start" && waiting:
			send(writer, map[string]any{"jsonrpc": "2.0", "method": "turn/started", "params": map[string]any{
				"threadId": "e2e-thread", "turn": map[string]string{"id": "e2e-turn", "status": "inProgress"},
			}})
			send(writer, map[string]any{"jsonrpc": "2.0", "id": 9001, "method": "item/commandExecution/requestApproval", "params": map[string]string{"command": "e2e-fixture"}})
		case incoming.Method == "turn/start":
			// The real Agent forwards this asynchronous notification to the
			// real Runner API, which persists it in libSQL.
			send(writer, map[string]any{"jsonrpc": "2.0", "method": "item/agentMessage/delta", "params": map[string]any{"delta": "e2e-response-from-app-server"}})
			send(writer, map[string]any{"jsonrpc": "2.0", "method": "turn/completed", "params": map[string]any{"turn": map[string]string{"status": "completed"}}})
		case incoming.Method == "turn/interrupt":
			send(writer, map[string]any{"jsonrpc": "2.0", "method": "turn/completed", "params": map[string]any{"turn": map[string]string{"status": "interrupted"}}})
		}
	}
	if err := reader.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "e2e app-server: stdin read failed")
		os.Exit(1)
	}
}

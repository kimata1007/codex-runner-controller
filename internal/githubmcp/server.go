// Package githubmcp exposes the policy-scoped GitHub Proxy to Codex over MCP.
package githubmcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kimata1007/codex-runner-controller/internal/mcpstdio"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const endpoint = "http://198.18.0.11:8080"

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	return serve(ctx, input, output, endpoint, &http.Client{Timeout: 45 * time.Second})
}

func serve(ctx context.Context, input io.Reader, output io.Writer, baseURL string, client *http.Client) error {
	reader, writer := bufio.NewReader(input), bufio.NewWriter(output)
	for {
		line, err := mcpstdio.ReadLine(reader)
		if len(bytes.TrimSpace(line)) != 0 {
			var request message
			if json.Unmarshal(line, &request) != nil || request.JSONRPC != "2.0" {
				return errors.New("invalid MCP message")
			}
			if request.ID != nil {
				result, rpcErr := dispatch(ctx, client, baseURL, request)
				encoded, encodeErr := json.Marshal(rpcResponse{JSONRPC: "2.0", ID: request.ID, Result: result, Error: rpcErr})
				if encodeErr != nil {
					return encodeErr
				}
				if _, err = writer.Write(append(encoded, '\n')); err != nil {
					return err
				}
				if err = writer.Flush(); err != nil {
					return err
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func dispatch(ctx context.Context, client *http.Client, baseURL string, request message) (any, *rpcError) {
	switch request.Method {
	case "initialize":
		return map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]string{"name": "codex-github-proxy", "version": "1.0.0"}}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": []any{
			tool("pull_request_create", "Create a draft pull request from a session branch.", map[string]any{
				"head":  stringSchema("Session branch name, under the authorized codex/<session-uid>/ prefix."),
				"title": stringSchema("Pull request title."), "body": stringSchema("Pull request description."),
			}, "head", "title", "body"),
			tool("pull_request_update", "Update the title or body of a pull request owned by this session branch.", map[string]any{
				"number": map[string]string{"type": "integer", "description": "Pull request number."},
				"title":  stringSchema("New title."), "body": stringSchema("New description."),
			}, "number"),
			tool("pull_request_status", "Read the status of a pull request owned by this session branch.", map[string]any{
				"number": map[string]string{"type": "integer", "description": "Pull request number."},
			}, "number"),
			tool("pull_request_checks", "Read CI check results for a pull request owned by this session branch.", map[string]any{
				"number": map[string]string{"type": "integer", "description": "Pull request number."},
			}, "number"),
		}}, nil
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(request.Params, &params) != nil {
			return nil, &rpcError{Code: -32602, Message: "invalid tool parameters"}
		}
		return callTool(ctx, client, strings.TrimRight(baseURL, "/"), params.Name, params.Arguments)
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found"}
	}
}

func callTool(ctx context.Context, client *http.Client, baseURL, name string, raw json.RawMessage) (any, *rpcError) {
	method, path, body := http.MethodGet, "", any(nil)
	switch name {
	case "pull_request_create":
		var args struct {
			Head  string `json:"head"`
			Title string `json:"title"`
			Body  string `json:"body"`
		}
		if json.Unmarshal(raw, &args) != nil || args.Head == "" || strings.TrimSpace(args.Title) == "" {
			return nil, &rpcError{Code: -32602, Message: "head and title are required"}
		}
		method, path, body = http.MethodPost, "/v1/pulls", map[string]string{"head": args.Head, "title": args.Title, "body": args.Body}
	case "pull_request_update", "pull_request_status", "pull_request_checks":
		var args struct {
			Number uint64  `json:"number"`
			Title  *string `json:"title"`
			Body   *string `json:"body"`
		}
		if json.Unmarshal(raw, &args) != nil || args.Number == 0 {
			return nil, &rpcError{Code: -32602, Message: "valid pull request number is required"}
		}
		action := ""
		if name == "pull_request_checks" {
			action = "/checks"
		}
		path = "/v1/pulls/" + strconv.FormatUint(args.Number, 10) + action
		if name == "pull_request_update" {
			if args.Title == nil && args.Body == nil {
				return nil, &rpcError{Code: -32602, Message: "title or body is required"}
			}
			method = http.MethodPatch
			body = struct {
				Title *string `json:"title,omitempty"`
				Body  *string `json:"body,omitempty"`
			}{args.Title, args.Body}
		}
	default:
		return nil, &rpcError{Code: -32602, Message: "unknown GitHub Proxy tool"}
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, &rpcError{Code: -32603, Message: "could not encode GitHub request"}
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, baseURL+path, reader)
	if err != nil {
		return nil, &rpcError{Code: -32603, Message: "GitHub Proxy unavailable"}
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, &rpcError{Code: -32603, Message: "GitHub Proxy unavailable"}
	}
	defer response.Body.Close()
	const maxResponse = 1 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil || len(data) > maxResponse {
		return nil, &rpcError{Code: -32603, Message: "GitHub Proxy response limit exceeded"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return map[string]any{"isError": true, "content": []any{map[string]string{"type": "text", "text": fmt.Sprintf("GitHub Proxy rejected the operation (HTTP %d)", response.StatusCode)}}}, nil
	}
	return map[string]any{"content": []any{map[string]string{"type": "text", "text": string(data)}}}, nil
}

func tool(name, description string, properties map[string]any, required ...string) map[string]any {
	return map[string]any{"name": name, "description": description,
		"inputSchema": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}}
}

func stringSchema(description string) map[string]string {
	return map[string]string{"type": "string", "description": description}
}

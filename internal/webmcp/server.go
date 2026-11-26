// Package webmcp exposes the identity-authorized Web Proxy as Codex MCP tools.
package webmcp

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
	"net/url"
	"strings"
	"time"
)

const endpoint = "http://198.18.0.12:8080"

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve implements MCP stdio using newline-delimited JSON-RPC messages.
func Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	return serve(ctx, input, output, endpoint, &http.Client{Timeout: 45 * time.Second})
}

func serve(ctx context.Context, input io.Reader, output io.Writer, baseURL string, client *http.Client) error {
	reader := bufio.NewReader(input)
	writer := bufio.NewWriter(output)
	for {
		line, err := mcpstdio.ReadLine(reader)
		if len(bytes.TrimSpace(line)) != 0 {
			var request message
			if json.Unmarshal(line, &request) != nil || request.JSONRPC != "2.0" {
				return errors.New("invalid MCP message")
			}
			if request.ID == nil { // Notifications have no response.
				if request.Method == "exit" {
					return nil
				}
			} else {
				result, rpcErr := dispatch(ctx, client, baseURL, request)
				answer := response{JSONRPC: "2.0", ID: request.ID, Result: result, Error: rpcErr}
				encoded, encodeErr := json.Marshal(answer)
				if encodeErr != nil {
					return encodeErr
				}
				if _, writeErr := writer.Write(append(encoded, '\n')); writeErr != nil {
					return writeErr
				}
				if flushErr := writer.Flush(); flushErr != nil {
					return flushErr
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
			"serverInfo": map[string]string{"name": "codex-web-proxy", "version": "1.0.0"}}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": []any{
			map[string]any{"name": "web_search", "description": "Search the public web through the session-authorized Web Proxy.",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]string{"type": "string", "description": "Search query"}}, "required": []string{"query"}, "additionalProperties": false}},
			map[string]any{"name": "web_fetch", "description": "Fetch a public HTTPS page through the session-authorized Web Proxy.",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"url": map[string]string{"type": "string", "description": "Public HTTPS URL"}}, "required": []string{"url"}, "additionalProperties": false}},
		}}, nil
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(request.Params, &params) != nil {
			return nil, &rpcError{Code: -32602, Message: "invalid tool parameters"}
		}
		path, key, value := "", "", ""
		switch params.Name {
		case "web_search":
			var args struct {
				Query string `json:"query"`
			}
			if json.Unmarshal(params.Arguments, &args) != nil || strings.TrimSpace(args.Query) == "" || len(args.Query) > 600 {
				return nil, &rpcError{Code: -32602, Message: "invalid search query"}
			}
			path, key, value = "/v1/search", "q", args.Query
		case "web_fetch":
			var args struct {
				URL string `json:"url"`
			}
			if json.Unmarshal(params.Arguments, &args) != nil || !publicHTTPSURL(args.URL) {
				return nil, &rpcError{Code: -32602, Message: "invalid public HTTPS URL"}
			}
			path, key, value = "/v1/fetch", "url", args.URL
		default:
			return nil, &rpcError{Code: -32602, Message: "unknown Web Proxy tool"}
		}
		query := url.Values{key: {value}}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+path+"?"+query.Encode(), nil)
		if err != nil {
			return nil, &rpcError{Code: -32603, Message: "Web Proxy request failed"}
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, &rpcError{Code: -32603, Message: "Web Proxy unavailable"}
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return map[string]any{"isError": true, "content": []any{map[string]string{"type": "text", "text": fmt.Sprintf("Web Proxy rejected the request (HTTP %d)", response.StatusCode)}}}, nil
		}
		const maxResult = 4 << 20
		body, err := io.ReadAll(io.LimitReader(response.Body, maxResult+1))
		if err != nil || len(body) > maxResult {
			return nil, &rpcError{Code: -32603, Message: "Web Proxy response exceeded the tool limit"}
		}
		return map[string]any{"content": []any{map[string]string{"type": "text", "text": string(body)}}}, nil
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found"}
	}
}

func publicHTTPSURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == "" && len(raw) <= 4096
}

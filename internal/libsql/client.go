package libsql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

type Client struct {
	httpClient  *http.Client
	pipelineURL string
	token       string

	mu    sync.Mutex
	baton *string
}

type Value struct {
	Type   string `json:"type"`
	Value  string `json:"value,omitempty"`
	Base64 string `json:"base64,omitempty"`
}

type ExecResult struct {
	Columns          []string
	Rows             [][]Value
	AffectedRowCount int
}

type pipelineRequest struct {
	Baton    *string         `json:"baton"`
	Requests []streamRequest `json:"requests"`
}

type streamRequest struct {
	Type string     `json:"type"`
	Stmt *statement `json:"stmt,omitempty"`
	SQL  string     `json:"sql,omitempty"`
}

type statement struct {
	SQL      string  `json:"sql"`
	Args     []Value `json:"args,omitempty"`
	WantRows bool    `json:"want_rows"`
}

type pipelineResponse struct {
	Baton   *string        `json:"baton"`
	BaseURL string         `json:"base_url"`
	Results []streamResult `json:"results"`
}

type streamResult struct {
	Type     string          `json:"type"`
	Response json.RawMessage `json:"response"`
	Error    *hranaError     `json:"error,omitempty"`
}

type hranaError struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

type executeResponse struct {
	Type   string          `json:"type"`
	Result statementResult `json:"result"`
}

type statementResult struct {
	Columns          []column  `json:"cols"`
	Rows             [][]Value `json:"rows"`
	AffectedRowCount int       `json:"affected_row_count"`
}

type column struct {
	Name string `json:"name"`
}

func NewClient(baseURL, token string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		httpClient:  http.DefaultClient,
		pipelineURL: baseURL + "/v2/pipeline",
		token:       token,
	}
}

func (c *Client) WithHTTPClient(httpClient *http.Client) *Client {
	if httpClient != nil {
		c.httpClient = httpClient
	}
	return c
}

func (c *Client) Exec(ctx context.Context, sql string, args []Value, wantRows bool) (ExecResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	response, err := c.pipeline(ctx, []streamRequest{{
		Type: "execute",
		Stmt: &statement{SQL: sql, Args: args, WantRows: wantRows},
	}})
	if err != nil {
		return ExecResult{}, err
	}
	if len(response.Results) != 1 {
		return ExecResult{}, fmt.Errorf("libsql: expected one result, got %d", len(response.Results))
	}
	result := response.Results[0]
	if result.Type == "error" {
		return ExecResult{}, fmt.Errorf("libsql: %s", result.Error.Message)
	}
	var execute executeResponse
	if err := json.Unmarshal(result.Response, &execute); err != nil {
		return ExecResult{}, fmt.Errorf("libsql: decode execute response: %w", err)
	}
	columns := make([]string, len(execute.Result.Columns))
	for i, column := range execute.Result.Columns {
		columns[i] = column.Name
	}
	return ExecResult{Columns: columns, Rows: execute.Result.Rows, AffectedRowCount: execute.Result.AffectedRowCount}, nil
}

func (c *Client) ApplySchema(ctx context.Context, schema string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	response, err := c.pipeline(ctx, []streamRequest{{Type: "sequence", SQL: schema}})
	if err != nil {
		return err
	}
	if len(response.Results) != 1 || response.Results[0].Type == "error" {
		if len(response.Results) == 1 && response.Results[0].Error != nil {
			return fmt.Errorf("libsql: apply schema: %s", response.Results[0].Error.Message)
		}
		return fmt.Errorf("libsql: apply schema failed")
	}
	return nil
}

func (c *Client) pipeline(ctx context.Context, requests []streamRequest) (pipelineResponse, error) {
	payload, err := json.Marshal(pipelineRequest{Baton: c.baton, Requests: requests})
	if err != nil {
		return pipelineResponse{}, fmt.Errorf("libsql: encode pipeline request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.pipelineURL, bytes.NewReader(payload))
	if err != nil {
		return pipelineResponse{}, fmt.Errorf("libsql: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return pipelineResponse{}, fmt.Errorf("libsql: pipeline request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return pipelineResponse{}, fmt.Errorf("libsql: pipeline returned HTTP %s", resp.Status)
	}
	var response pipelineResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return pipelineResponse{}, fmt.Errorf("libsql: decode pipeline response: %w", err)
	}
	c.baton = response.Baton
	if response.BaseURL != "" {
		c.pipelineURL = strings.TrimRight(response.BaseURL, "/") + "/v2/pipeline"
	}
	return response, nil
}

func Text(value string) Value {
	return Value{Type: "text", Value: value}
}

func Integer(value int64) Value {
	return Value{Type: "integer", Value: fmt.Sprintf("%d", value)}
}

func Null() Value {
	return Value{Type: "null"}
}

func StringValue(value Value) (string, error) {
	if value.Type != "text" && value.Type != "integer" {
		return "", fmt.Errorf("libsql: expected text or integer, got %q", value.Type)
	}
	return value.Value, nil
}

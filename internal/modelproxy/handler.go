package modelproxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/zerotrust"
	"k8s.io/apimachinery/pkg/types"
)

type SessionAuthorizer interface {
	Session(context.Context, zerotrust.Identity) (zerotrust.SessionAuthorization, error)
}

type Accounting interface {
	Reserve(context.Context, types.UID, int64, int64, int64) error
	Refund(context.Context, types.UID, int64) error
}

type Handler struct {
	Authorizer SessionAuthorizer
	Accounting Accounting
	Credential func() (string, error)
	Domain     string
	client     *http.Client
	slots      chan struct{}
}

func New(authorizer SessionAuthorizer, accounting Accounting, credential func() (string, error), domain string) *Handler {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.ResponseHeaderTimeout = 45 * time.Second
	transport.MaxConnsPerHost = 64
	return &Handler{Authorizer: authorizer, Accounting: accounting, Credential: credential, Domain: domain,
		client: &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}}, slots: make(chan struct{}, 64)}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet && r.URL.Path == "/readyz" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" || r.URL.RawQuery != "" {
		http.NotFound(w, r)
		return
	}
	source, ok := zerotrust.LocalIdentity(r.Context())
	if !ok {
		http.Error(w, "Model request denied", http.StatusForbidden)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Minute)
	defer cancel()
	policy, err := h.Authorizer.Session(ctx, source)
	target := zerotrust.Identity("spiffe://" + h.Domain + "/proxy/model")
	if err != nil || policy.Model == nil || !policy.Policy.Allows(source, target, zerotrust.ProxyPort, time.Now()) {
		http.Error(w, "Model request denied", http.StatusForbidden)
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		http.Error(w, "Model request limit reached", http.StatusTooManyRequests)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "Invalid model request", http.StatusBadRequest)
		return
	}
	body, model, stream, price, err := prepare(raw, policy.Model, policy.Workload.SessionUID)
	if err != nil {
		http.Error(w, "Model request denied", http.StatusForbidden)
		return
	}
	key, err := h.Credential()
	key = strings.TrimSpace(key)
	if err != nil || key == "" || strings.ContainsAny(key, "\r\n \t") {
		http.Error(w, "Model provider unavailable", http.StatusServiceUnavailable)
		return
	}
	reservation := price.cost(int64(len(body)), price.MaxOutputTokens)
	if err := h.Accounting.Reserve(ctx, policy.Workload.SessionUID, reservation, policy.Model.BudgetMicros, policy.Model.RequestsPerMinute); err != nil {
		if errors.Is(err, errBudgetExceeded) {
			http.Error(w, "Model budget or rate limit reached", http.StatusTooManyRequests)
		} else {
			http.Error(w, "Model accounting unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	req := &http.Request{Method: http.MethodPost, URL: &url.URL{Scheme: "https", Host: "api.openai.com", Path: "/v1/responses"},
		Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body))}
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	response, err := h.client.Do(req)
	if err != nil {
		http.Error(w, "Model provider unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusTooManyRequests {
			http.Error(w, "Model provider rate limit reached", http.StatusTooManyRequests)
		} else {
			http.Error(w, "Model provider rejected the request", http.StatusBadGateway)
		}
		return
	}
	media, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	var used tokenUsage
	requestID := response.Header.Get("x-request-id")
	if stream && media == "text/event-stream" {
		w.Header().Set("Content-Type", "text/event-stream")
		used = forwardStream(w, response.Body)
	} else if !stream && media == "application/json" {
		payload, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
		var result struct {
			Usage tokenUsage      `json:"usage"`
			Error json.RawMessage `json:"error"`
		}
		if err != nil || len(payload) > 8<<20 || json.Unmarshal(payload, &result) != nil ||
			(len(result.Error) > 0 && string(result.Error) != "null") {
			http.Error(w, "Invalid model provider response", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
		used = result.Usage
	} else {
		http.Error(w, "Invalid model provider response", http.StatusBadGateway)
		return
	}
	if used.Input > 0 && used.Input <= int64(len(body)) && used.Output >= 0 && used.Output <= price.MaxOutputTokens {
		settle, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		err = h.Accounting.Refund(settle, policy.Workload.SessionUID, reservation-price.cost(used.Input, used.Output))
		stop()
	}
	slog.InfoContext(ctx, "model response", "operation", "model.responses", "source_identity", source,
		"destination_identity", target, "request_id", requestID, "model", model, "policy_version", policy.Version,
		"input_tokens", used.Input, "output_tokens", used.Output, "accounting_settled", err == nil && used.Input > 0)
}

type tokenUsage struct {
	Input  int64 `json:"input_tokens"`
	Output int64 `json:"output_tokens"`
}

func forwardStream(w http.ResponseWriter, body io.Reader) tokenUsage {
	scanner := bufio.NewScanner(io.LimitReader(body, 32<<20))
	scanner.Buffer(make([]byte, 16<<10), 1<<20)
	flush := http.NewResponseController(w)
	var event bytes.Buffer
	var usage tokenUsage
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) > 0 {
			if event.Len()+len(line) >= 1<<20 {
				break
			}
			event.Write(line)
			event.WriteByte('\n')
			continue
		}
		var response struct {
			Type     string `json:"type"`
			Response struct {
				Usage tokenUsage `json:"usage"`
			} `json:"response"`
		}
		for _, part := range bytes.Split(event.Bytes(), []byte("\n")) {
			if p, ok := bytes.CutPrefix(part, []byte("data:")); ok {
				_ = json.Unmarshal(p, &response)
				break
			}
		}
		if response.Type == "error" || response.Type == "response.failed" {
			_, _ = io.WriteString(w, "event: error\ndata: {\"type\":\"error\",\"message\":\"Model provider failed\"}\n\n")
			_ = flush.Flush()
			return usage
		}
		if response.Type == "response.completed" {
			usage = response.Response.Usage
		}
		event.WriteByte('\n')
		if _, err := w.Write(event.Bytes()); err != nil || flush.Flush() != nil {
			return usage
		}
		event.Reset()
	}
	return usage
}

type rateCard v1alpha1.ModelGrant

func (p rateCard) cost(input, output int64) int64 {
	return max(1, (input*p.InputMicrosPerMillion+output*p.OutputMicrosPerMillion+999999)/1000000)
}

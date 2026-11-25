package modelproxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/zerotrust"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type fakeAuthorizer struct {
	record zerotrust.SessionAuthorization
}

func (a fakeAuthorizer) Session(_ context.Context, id zerotrust.Identity) (zerotrust.SessionAuthorization, error) {
	if id != a.record.Identity {
		return zerotrust.SessionAuthorization{}, zerotrust.ErrPolicyDenied
	}
	return a.record, nil
}

type modelRoundTrip func(*http.Request) (*http.Response, error)

func (f modelRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestModelProxyForwardsAnAuthorizedSessionOverUnixSocket(t *testing.T) {
	const source zerotrust.Identity = "spiffe://codex.local/session/test/pod/uid-test"
	const proxyID zerotrust.Identity = "spiffe://codex.local/proxy/model"
	expires := time.Now().Add(time.Minute)
	policy, err := zerotrust.NewPolicy("codex.local", expires, []zerotrust.Grant{{Source: source, Destination: proxyID, Port: 8080}})
	if err != nil {
		t.Fatal(err)
	}
	record := zerotrust.SessionAuthorization{Identity: source, Policy: policy, Version: 1,
		Workload: v1alpha1.SessionWorkload{SessionUID: "test-session"},
		Model: &v1alpha1.ModelPolicy{BudgetMicros: 10000, RequestsPerMinute: 3,
			Models: []v1alpha1.ModelGrant{{Name: "configured-model", MaxInputBytes: 4096, MaxOutputTokens: 128,
				InputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 2000000}}}}
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	accounting := BudgetStore{Kube: fake.NewClientBuilder().WithScheme(scheme).Build(), Namespace: "usage"}
	handler := New(fakeAuthorizer{record}, accounting, func() (string, error) { return "fixture-upstream-key", nil }, "codex.local")
	var calls atomic.Int32
	handler.client.Transport = modelRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.String() != "https://api.openai.com/v1/responses" || r.Header.Get("Authorization") != "Bearer fixture-upstream-key" ||
			r.Header.Get("X-Session-Identity") != "" {
			t.Error("incorrect upstream identity or credential forwarding")
		}
		_ = r.Body.Close()
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":12,\"output_tokens\":3}}}\n\n"))}, nil
	})
	dir, err := os.MkdirTemp("", "cz-model-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "m.sock")
	listener, err := zerotrust.ListenPrivate(socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler, ConnContext: zerotrust.ContextWithLocalIdentity}
	go server.Serve(zerotrust.AuthenticatedListener(listener, "codex.local"))
	t.Cleanup(func() { _ = server.Close() })
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(conn, string(source)+"\n"); err != nil {
			_ = conn.Close()
			return nil, err
		}
		return conn, nil
	}}
	defer transport.CloseIdleConnections()
	downstream := &http.Client{Transport: transport, Timeout: 4 * time.Second}
	res, err := downstream.Post("http://fixture/v1/responses", "application/json",
		strings.NewReader(`{"model":"configured-model","input":"hello","stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != http.StatusOK || !strings.Contains(string(data), "response.completed") ||
		strings.Contains(string(data), "fixture-upstream-key") || calls.Load() != 1 {
		t.Fatalf("unexpected proxied response %d, %s (%v)", res.StatusCode, data, err)
	}
}

func TestModelRequestIsScoped(t *testing.T) {
	policy := &v1alpha1.ModelPolicy{BudgetMicros: 1000, RequestsPerMinute: 2,
		Models: []v1alpha1.ModelGrant{{Name: "configured-model", MaxInputBytes: 4096, MaxOutputTokens: 512,
			InputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 2000000}}}
	body, _, stream, rate, err := prepare([]byte(`{"model":"configured-model","input":"Inspect the project","stream":true,"store":true,"service_tier":"priority"}`), policy, "test-session")
	if err != nil || !stream || rate.MaxOutputTokens != 512 {
		t.Fatalf("unexpected request decision: %v", err)
	}
	var encoded map[string]any
	if err := json.Unmarshal(body, &encoded); err != nil || encoded["store"] != false || encoded["service_tier"] != "default" {
		t.Fatalf("request privacy settings not enforced: %s, %v", body, err)
	}
	for _, request := range []string{
		`{"model":"different-model","input":"hello"}`,
		`{"model":"configured-model","input":"hello","max_output_tokens":513}`,
		`{"model":"configured-model","input":"hello","previous_response_id":"resp_123"}`,
		`{"model":"configured-model","input":"hello","tools":[{"type":"web_search"}]}`,
		`{"model":"configured-model","input":[{"type":"message","content":[{"type":"input_image","image_url":"https://example.com/image"}]}]}`,
	} {
		if _, _, _, _, err := prepare([]byte(request), policy, "test-session"); !errors.Is(err, errInvalidRequest) {
			t.Errorf("accepted an unapproved request %s, error %v", request, err)
		}
	}
}

func TestBudgetIsSharedAcrossConcurrentRequestsAndRestarts(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).Build()
	store := BudgetStore{Kube: kube, Namespace: "accounting"}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := store.Reserve(t.Context(), "test-session", 50, 100, 60)
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, errBudgetExceeded) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 2 {
		t.Fatalf("reserved %d requests, want 2", successes.Load())
	}
	store = BudgetStore{Kube: kube, Namespace: "accounting"}
	if err := store.Reserve(t.Context(), "test-session", 1, 100, 60); !errors.Is(err, errBudgetExceeded) {
		t.Fatalf("persisted limit was lost: %v", err)
	}
	if err := store.Refund(t.Context(), "test-session", 25); err != nil {
		t.Fatal(err)
	}
	if err := store.Reserve(t.Context(), "test-session", 25, 100, 60); err != nil {
		t.Fatal(err)
	}
}

func TestStreamForwardsEventsAndRedactsProviderErrors(t *testing.T) {
	const output = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n"
	const complete = "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":14,\"output_tokens\":3}}}\n\n"
	w := httptest.NewRecorder()
	usage := forwardStream(w, strings.NewReader(output+complete))
	if usage.Input != 14 || usage.Output != 3 || w.Body.String() != output+complete || !w.Flushed {
		t.Fatalf("invalid event forwarding: %s, %+v", w.Body.String(), usage)
	}
	w = httptest.NewRecorder()
	forwardStream(w, strings.NewReader("data: {\"type\":\"error\",\"message\":\"upstream confidential detail\"}\n\n"))
	if strings.Contains(w.Body.String(), "confidential") || !strings.Contains(w.Body.String(), "Model provider failed") {
		t.Fatalf("provider failure not sanitized: %s", w.Body)
	}
}

func TestNoHTTPHeaderCanAssertIdentity(t *testing.T) {
	proxy := New(nil, nil, nil, "codex.local")
	w := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request.Header.Set("X-Session-Identity", "spiffe://codex.local/session/test/pod/test-pod")
	proxy.ServeHTTP(w, request)
	if w.Code != http.StatusForbidden {
		t.Fatalf("spoofed HTTP header returned %d", w.Code)
	}
}

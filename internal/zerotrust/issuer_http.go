package zerotrust

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/types"
)

const (
	issuancePath       = "/v1/certificates"
	trustBundlePath    = "/v1/trust-bundle"
	maxIssueBodyBytes  = 24 << 10
	issuanceAPITimeout = 8 * time.Second
)

type certificateRequest struct {
	PodUID types.UID `json:"podUID,omitempty"`
	CSR    []byte    `json:"csr"`
}

type certificateResponse struct {
	Certificate []byte `json:"certificate"`
}

type issuerHTTP struct {
	ca       CAFiles
	nodes    NodeAuthenticator
	policies PolicyStore
	limit    *rate.Limiter
	slots    chan struct{}
}

func NewIssuerHandler(ca CAFiles, nodes NodeAuthenticator, policies PolicyStore) http.Handler {
	h := &issuerHTTP{ca: ca, nodes: nodes, policies: policies, limit: rate.NewLimiter(40, 60), slots: make(chan struct{}, 32)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+issuancePath, h.issue)
	mux.HandleFunc("GET "+trustBundlePath, h.bundle)
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if _, err := ca.Issuer(time.Now()); err != nil {
			http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.TLS == nil || r.TLS.Version != tls.VersionTLS13 {
			http.Error(w, "TLS 1.3 required", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (h *issuerHTTP) issue(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), issuanceAPITimeout)
	defer cancel()
	requestID := rand.Text()
	var source Identity
	var destination Identity
	var version int64
	record := func(decision, reason string) {
		slog.InfoContext(ctx, "certificate issuance", "request_id", requestID, "operation", "identity.issue",
			"source_identity", source, "destination_identity", destination, "decision", decision,
			"reason", reason, "policy_version", version)
	}
	deny := func(status int, reason string) {
		record("deny", reason)
		http.Error(w, http.StatusText(status), status)
	}
	if !h.limit.Allow() {
		deny(http.StatusTooManyRequests, "rate_limit")
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		deny(http.StatusTooManyRequests, "concurrency_limit")
		return
	}
	authorization := r.Header.Values("Authorization")
	if len(authorization) != 1 {
		deny(http.StatusUnauthorized, "node_token_missing")
		return
	}
	token, ok := strings.CutPrefix(authorization[0], "Bearer ")
	if !ok || token == "" || strings.ContainsAny(token, " \t,") {
		deny(http.StatusUnauthorized, "node_token_invalid")
		return
	}
	var request certificateRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxIssueBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || decoder.Decode(new(json.RawMessage)) != io.EOF {
		deny(http.StatusBadRequest, "request_invalid")
		return
	}
	node, err := h.nodes.authenticate(ctx, token)
	if err != nil {
		deny(http.StatusUnauthorized, "node_unverified")
		return
	}
	source = Identity("spiffe://" + h.ca.Domain + "/node/" + string(node.uid))
	now := time.Now()
	issuer, err := h.ca.Issuer(now)
	if err != nil {
		deny(http.StatusServiceUnavailable, "signer_unavailable")
		return
	}
	attest := func(ctx context.Context) (Attestation, error) {
		if request.PodUID == "" {
			destination = source
			return Attestation{Identity: source}, nil
		}
		policy, err := h.policies.Lookup(ctx, request.PodUID, time.Now())
		if err != nil || policy.Workload.NodeUID != node.uid || policy.Workload.NodeName != node.name {
			return Attestation{}, ErrAttestationDenied
		}
		destination, version = policy.Identity, policy.Version
		return Attestation{Identity: policy.Identity, Deadline: policy.Expires}, nil
	}
	der, err := issuer.Issue(ctx, attest, request.CSR, now)
	switch {
	case errors.Is(err, ErrAttestationDenied):
		deny(http.StatusForbidden, "workload_unverified")
	case errors.Is(err, ErrInvalidCSR):
		deny(http.StatusBadRequest, "csr_invalid")
	case err != nil:
		deny(http.StatusServiceUnavailable, "signing_failed")
	default:
		record("allow", "attested")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(certificateResponse{Certificate: der})
	}
}

func (h *issuerHTTP) bundle(w http.ResponseWriter, _ *http.Request) {
	if !h.limit.Allow() {
		http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
		return
	}
	bundle, err := h.ca.TrustBundle()
	if err != nil {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	_, _ = w.Write(bundle)
}

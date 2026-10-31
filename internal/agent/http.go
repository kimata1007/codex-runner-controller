package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// SessionAPI is the small boundary exposed by the live App Server session.
type SessionAPI interface {
	StartThread(context.Context, ThreadStartParams) (Thread, error)
	ResumeThread(context.Context, string, ThreadStartParams) (Thread, error)
	StartTurn(context.Context, string, []TurnInput) error
}

type HTTPServer struct {
	session SessionAPI
}

func NewHTTPHandler(session SessionAPI) http.Handler {
	server := &HTTPServer{session: session}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", server.health)
	mux.HandleFunc("/v1/threads", server.threads)
	mux.HandleFunc("/v1/threads/", server.thread)
	return mux
}

type threadRequest struct {
	Model          string `json:"model,omitempty"`
	CWD            string `json:"cwd,omitempty"`
	ApprovalPolicy string `json:"approvalPolicy,omitempty"`
}

type turnRequest struct {
	Text string `json:"text"`
}

func (s *HTTPServer) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *HTTPServer) threads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request threadRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	thread, err := s.session.StartThread(r.Context(), ThreadStartParams{
		Model:          request.Model,
		CWD:            request.CWD,
		ApprovalPolicy: request.ApprovalPolicy,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, thread)
}

func (s *HTTPServer) thread(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/threads/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	threadID, action := parts[0], parts[1]
	if threadID == "" {
		writeError(w, http.StatusBadRequest, "thread id is required")
		return
	}

	switch {
	case action == "resume" && r.Method == http.MethodPost:
		var request threadRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		thread, err := s.session.ResumeThread(r.Context(), threadID, ThreadStartParams{
			Model:          request.Model,
			CWD:            request.CWD,
			ApprovalPolicy: request.ApprovalPolicy,
		})
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, thread)
	case action == "turns" && r.Method == http.MethodPost:
		var request turnRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if strings.TrimSpace(request.Text) == "" {
			writeError(w, http.StatusBadRequest, "text is required")
			return
		}
		if err := s.session.StartTurn(r.Context(), threadID, []TurnInput{{Type: "text", Text: request.Text}}); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

var _ SessionAPI = (*Session)(nil)

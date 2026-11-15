package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/labstack/echo/v4"
)

const maxRequestBodyBytes = 1 << 20

// SessionAPI is the small boundary exposed by the live App Server session.
type SessionAPI interface {
	StartThread(context.Context, ThreadStartParams) (Thread, error)
	ResumeThread(context.Context, string, ThreadStartParams) (Thread, error)
	StartTurn(context.Context, string, []TurnInput) error
}

type HTTPServer struct {
	session   SessionAPI
	tracker   *RunTracker
	approvals ApprovalResolver
}

type ApprovalResolver interface {
	Resolve(string, any) error
}

func NewHTTPHandler(session SessionAPI) *echo.Echo {
	return NewHTTPHandlerWithTracker(session, NewRunTracker())
}

func NewHTTPHandlerWithTracker(session SessionAPI, tracker *RunTracker) *echo.Echo {
	return NewHTTPHandlerWithTrackerAndApprovals(session, tracker, nil)
}

func NewHTTPHandlerWithTrackerAndApprovals(session SessionAPI, tracker *RunTracker, approvals ApprovalResolver) *echo.Echo {
	return NewHTTPHandlerWithAuthBroker(session, tracker, approvals, false)
}

func NewHTTPHandlerWithAuthBroker(session SessionAPI, tracker *RunTracker, approvals ApprovalResolver, authBroker bool) *echo.Echo {
	if tracker == nil {
		tracker = NewRunTracker()
	}
	server := &HTTPServer{session: session, tracker: tracker, approvals: approvals}
	e := echo.New()
	e.HideBanner = true
	e.GET("/healthz", server.health)
	e.GET("/readyz", server.health)
	e.POST("/v1/threads", server.startThread)
	e.POST("/v1/threads/:id/resume", server.resumeThread)
	e.POST("/v1/threads/:id/turns", server.startTurn)
	e.POST("/v1/approvals/:id", server.approval)
	e.POST("/v1/auth/tokens", server.setAuthTokens)
	if authBroker {
		e.POST("/v1/auth/device-login", server.startDeviceLogin)
		e.GET("/v1/auth/device-login/:loginID", server.deviceLoginStatus)
		e.DELETE("/v1/auth/device-login/:loginID", server.cancelDeviceLogin)
		e.GET("/v1/auth/credentials", server.authCredentials)
		e.DELETE("/v1/auth/account", server.logoutAccount)
	}
	return e
}

type threadRequest struct {
	Model          string `json:"model,omitempty"`
	CWD            string `json:"cwd,omitempty"`
	ApprovalPolicy string `json:"approvalPolicy,omitempty"`
}

type turnRequest struct {
	Text  string `json:"text"`
	RunID string `json:"runId,omitempty"`
}

func (s *HTTPServer) health(c echo.Context) error {
	return writeJSON(c, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *HTTPServer) startThread(c echo.Context) error {
	var request threadRequest
	if err := decodeJSON(c, &request); err != nil {
		return err
	}
	thread, err := s.session.StartThread(c.Request().Context(), ThreadStartParams{
		Model:          request.Model,
		CWD:            request.CWD,
		ApprovalPolicy: request.ApprovalPolicy,
	})
	if err != nil {
		return writeError(c, http.StatusBadGateway, err.Error())
	}
	return writeJSON(c, http.StatusCreated, thread)
}

func (s *HTTPServer) resumeThread(c echo.Context) error {
	var request threadRequest
	if err := decodeJSON(c, &request); err != nil {
		return err
	}
	thread, err := s.session.ResumeThread(c.Request().Context(), c.Param("id"), ThreadStartParams{
		Model:          request.Model,
		CWD:            request.CWD,
		ApprovalPolicy: request.ApprovalPolicy,
	})
	if err != nil {
		return writeError(c, http.StatusBadGateway, err.Error())
	}
	return writeJSON(c, http.StatusOK, thread)
}

func (s *HTTPServer) startTurn(c echo.Context) error {
	var request turnRequest
	if err := decodeJSON(c, &request); err != nil {
		return err
	}
	request.Text = strings.TrimSpace(request.Text)
	if request.Text == "" {
		return writeError(c, http.StatusBadRequest, "text is required")
	}
	if request.RunID != "" {
		s.tracker.Set(request.RunID)
	}
	if err := s.session.StartTurn(c.Request().Context(), c.Param("id"), []TurnInput{{Type: "text", Text: request.Text}}); err != nil {
		return writeError(c, http.StatusBadGateway, err.Error())
	}
	return writeJSON(c, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (s *HTTPServer) approval(c echo.Context) error {
	requestID := strings.TrimSpace(c.Param("id"))
	if requestID == "" {
		return writeError(c, http.StatusBadRequest, "approval request id is required")
	}
	if s.approvals == nil {
		return writeError(c, http.StatusServiceUnavailable, "approval resolver is not configured")
	}
	var request struct {
		Response json.RawMessage `json:"response"`
		Decision string          `json:"decision"`
	}
	if err := decodeJSON(c, &request); err != nil {
		return err
	}
	response := request.Response
	if len(response) == 0 && request.Decision != "" {
		response, _ = json.Marshal(map[string]string{"decision": request.Decision})
	}
	if len(response) == 0 || !json.Valid(response) {
		return writeError(c, http.StatusBadRequest, "response must be valid JSON")
	}
	if err := s.approvals.Resolve(requestID, response); err != nil {
		return writeError(c, http.StatusNotFound, "approval request not found")
	}
	return writeJSON(c, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (s *HTTPServer) setAuthTokens(c echo.Context) error {
	account, ok := s.session.(interface {
		LoginWithChatGPTAuthTokens(context.Context, ChatGPTAuthTokens) error
	})
	if !ok {
		return writeError(c, http.StatusServiceUnavailable, "external ChatGPT authentication is unavailable")
	}
	var tokens ChatGPTAuthTokens
	if err := decodeJSON(c, &tokens); err != nil {
		return err
	}
	if err := account.LoginWithChatGPTAuthTokens(c.Request().Context(), tokens); err != nil {
		return writeError(c, http.StatusBadGateway, "failed to configure ChatGPT authentication")
	}
	return writeJSON(c, http.StatusOK, map[string]string{"status": "authenticated"})
}

func (s *HTTPServer) startDeviceLogin(c echo.Context) error {
	login, ok := s.session.(interface {
		StartDeviceLogin(context.Context) (DeviceLogin, error)
	})
	if !ok {
		return writeError(c, http.StatusServiceUnavailable, "device sign-in is unavailable")
	}
	result, err := login.StartDeviceLogin(c.Request().Context())
	if err != nil {
		return writeError(c, http.StatusBadGateway, "could not start ChatGPT sign-in")
	}
	return writeJSON(c, http.StatusCreated, result)
}

func (s *HTTPServer) deviceLoginStatus(c echo.Context) error {
	login, ok := s.session.(interface {
		ReadAccount(context.Context) (AccountStatus, error)
	})
	if !ok {
		return writeError(c, http.StatusServiceUnavailable, "device sign-in is unavailable")
	}
	status, err := login.ReadAccount(c.Request().Context())
	if err != nil {
		return writeError(c, http.StatusBadGateway, "could not read ChatGPT sign-in status")
	}
	return writeJSON(c, http.StatusOK, status)
}

func (s *HTTPServer) cancelDeviceLogin(c echo.Context) error {
	login, ok := s.session.(interface {
		CancelDeviceLogin(context.Context, string) error
	})
	if !ok {
		return writeError(c, http.StatusServiceUnavailable, "device sign-in is unavailable")
	}
	if err := login.CancelDeviceLogin(c.Request().Context(), c.Param("loginID")); err != nil {
		return writeError(c, http.StatusBadGateway, "could not cancel ChatGPT sign-in")
	}
	return writeJSON(c, http.StatusAccepted, map[string]string{"status": "cancelled"})
}

func (s *HTTPServer) authCredentials(c echo.Context) error {
	data, err := os.ReadFile(os.Getenv("CODEX_HOME") + "/auth.json")
	if err != nil {
		return writeError(c, http.StatusNotFound, "ChatGPT credentials are not available")
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return writeError(c, http.StatusBadGateway, "ChatGPT credentials are invalid")
	}
	return writeJSON(c, http.StatusOK, value)
}

func (s *HTTPServer) logoutAccount(c echo.Context) error {
	logout, ok := s.session.(interface{ Logout(context.Context) error })
	if !ok {
		return writeError(c, http.StatusServiceUnavailable, "sign-out is unavailable")
	}
	if err := logout.Logout(c.Request().Context()); err != nil {
		return writeError(c, http.StatusBadGateway, "could not sign out of ChatGPT")
	}
	return writeJSON(c, http.StatusOK, map[string]string{"status": "disconnected"})
}

func decodeJSON(c echo.Context, destination any) error {
	r := c.Request()
	r.Body = http.MaxBytesReader(c.Response().Writer, r.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		if err == io.EOF {
			return writeError(c, http.StatusBadRequest, "request body is required")
		}
		return writeError(c, http.StatusBadRequest, "invalid JSON request")
	}
	return nil
}

func writeJSON(c echo.Context, status int, value any) error {
	return c.JSON(status, value)
}

func writeError(c echo.Context, status int, message string) error {
	return writeJSON(c, status, map[string]string{"error": message})
}

var _ SessionAPI = (*Session)(nil)

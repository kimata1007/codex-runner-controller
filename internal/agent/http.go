package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
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
	session SessionAPI
}

func NewHTTPHandler(session SessionAPI) *echo.Echo {
	server := &HTTPServer{session: session}
	e := echo.New()
	e.HideBanner = true
	e.GET("/healthz", server.health)
	e.POST("/v1/threads", server.startThread)
	e.POST("/v1/threads/:id/resume", server.resumeThread)
	e.POST("/v1/threads/:id/turns", server.startTurn)
	return e
}

type threadRequest struct {
	Model          string `json:"model,omitempty"`
	CWD            string `json:"cwd,omitempty"`
	ApprovalPolicy string `json:"approvalPolicy,omitempty"`
}

type turnRequest struct {
	Text string `json:"text"`
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
	if err := s.session.StartTurn(c.Request().Context(), c.Param("id"), []TurnInput{{Type: "text", Text: request.Text}}); err != nil {
		return writeError(c, http.StatusBadGateway, err.Error())
	}
	return writeJSON(c, http.StatusAccepted, map[string]string{"status": "accepted"})
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

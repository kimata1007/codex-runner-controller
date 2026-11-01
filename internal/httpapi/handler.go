package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

const maxRequestBodyBytes = 1 << 20

func (s *Server) Routes() http.Handler {
	e := echo.New()
	e.HideBanner = true
	e.GET("/healthz", s.checkHealth)
	e.GET("/readyz", s.checkHealth)
	e.POST("/v1/conversations", s.createConversation)
	e.GET("/v1/conversations/:id", s.getConversation)
	e.GET("/v1/conversations/:id/messages", s.getMessages)
	e.POST("/v1/conversations/:id/messages", s.createMessage)
	return e
}

func (s *Server) checkHealth(c echo.Context) error {
	return writeJSON(c, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) createConversation(c echo.Context) error {
	conversation := s.store.CreateConversation()
	return writeJSON(c, http.StatusCreated, conversation)
}

func (s *Server) getConversation(c echo.Context) error {
	conversation, ok := s.store.GetConversation(c.Param("id"))
	if !ok {
		return writeError(c, http.StatusNotFound, "conversation not found")
	}
	return writeJSON(c, http.StatusOK, conversation)
}

func (s *Server) getMessages(c echo.Context) error {
	conversationID := c.Param("id")
	_, ok := s.store.GetConversation(conversationID)
	if !ok {
		return writeError(c, http.StatusNotFound, "conversation not found")
	}

	messages, _ := s.store.ListMessages(conversationID)
	return writeJSON(c, http.StatusOK, map[string]any{"messages": messages})
}

func (s *Server) createMessage(c echo.Context) error {
	var request MessageInput
	if err := decodeJSON(c, &request); err != nil {
		return err
	}
	request.Content = strings.TrimSpace(request.Content)
	if request.Content == "" {
		return writeError(c, http.StatusBadRequest, "content is required")
	}

	message, created, err := s.store.CreateMessage(c.Param("id"), request)
	if err != nil {
		return writeError(c, http.StatusNotFound, "conversation not found")
	}
	if created {
		return writeJSON(c, http.StatusAccepted, message)
	}
	return writeJSON(c, http.StatusOK, message)
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

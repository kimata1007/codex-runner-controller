package logging

import (
	"context"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var (
	httpStatus = regexp.MustCompile(`(?i)\b(?:returned|status)\s+(?:HTTP/\d(?:\.\d)?\s+)?([45][0-9]{2})\b`)
	rpcStatus  = regexp.MustCompile(`\bjsonrpc\s+(-?[0-9]{1,6})\b`)
)

// ErrorType returns a bounded error class suitable for logs. It deliberately
// avoids logging arbitrary upstream error strings, which may contain secrets
// or user content.
func ErrorType(err error) string {
	if err == nil {
		return "unknown"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "network_timeout"
	}
	message := err.Error()
	if match := rpcStatus.FindStringSubmatch(message); match != nil {
		return "jsonrpc_" + match[1]
	}
	if match := httpStatus.FindStringSubmatch(message); match != nil {
		if code, _ := strconv.Atoi(match[1]); code >= http.StatusBadRequest && code <= 599 {
			return "http_" + match[1]
		}
	}
	switch lower := strings.ToLower(message); {
	case strings.Contains(lower, "connection refused"):
		return "connection_refused"
	case strings.Contains(lower, "no such host"):
		return "dns_resolution"
	case strings.Contains(lower, "not found"):
		return "not_found"
	case strings.Contains(lower, "unauthorized"):
		return "unauthorized"
	case strings.Contains(lower, "forbidden"):
		return "forbidden"
	case strings.Contains(lower, "invalid json"):
		return "invalid_json"
	default:
		return "internal_error"
	}
}

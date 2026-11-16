// Package diagnostics emits safe, correlated error events.
// Raw error strings and request bodies must never be written to logs.
package diagnostics

import (
 "context"
 "crypto/rand"
 "encoding/hex"
 "errors"
 "fmt"
 "log"
 "net"
 "net/http"
 "regexp"
 "strconv"
 "strings"
)

var httpStatus = regexp.MustCompile(`(?i)\b(?:returned|status)\s+(?:HTTP/\d(?:\.\d)?\s+)?([45][0-9]{2})\b`)
var rpcStatus = regexp.MustCompile(`\bjsonrpc\s+(-?[0-9]{1,6})\b`)

// Classify returns only controlled, non-sensitive categories.
// Never include err.Error(), even after masking: arbitrary upstream errors can
// contain credentials, user prompts, repository URLs and private paths.
func Classify(err error) string {
 if err == nil { return "unknown" }
 if errors.Is(err, context.DeadlineExceeded) { return "deadline_exceeded" }
 if errors.Is(err, context.Canceled) { return "canceled" }
 var netErr net.Error
 if errors.As(err, &netErr) && netErr.Timeout() { return "network_timeout" }
 // The following patterns only extract bounded numeric protocol status codes.
 msg := err.Error()
 if m := rpcStatus.FindStringSubmatch(msg); m != nil { return "jsonrpc_" + m[1] }
 if m := httpStatus.FindStringSubmatch(msg); m != nil {
  n, _ := strconv.Atoi(m[1])
  if n >= http.StatusBadRequest && n <= 599 { return "http_" + m[1] }
 }
 low := strings.ToLower(msg)
 switch {
 case strings.Contains(low,"connection refused"): return "connection_refused"
 case strings.Contains(low,"no such host"): return "dns_resolution"
 case strings.Contains(low,"not found"): return "not_found"
 case strings.Contains(low,"unauthorized"): return "unauthorized"
 case strings.Contains(low,"forbidden"): return "forbidden"
 case strings.Contains(low,"invalid json"): return "invalid_json"
 default: return "internal_error"
 }
}

// Report writes a bounded, allowlisted event and returns a public correlation ID.
// operation must be a hard-coded identifier, never user-controlled data.
func Report(operation string, err error) string {
 var raw [12]byte
 if _, e := rand.Read(raw[:]); e != nil {
  // Avoid reporting the random generator error, which may contain details.
  log.Printf("diagnostic generation failed")
  return "unavailable"
 }
 id := hex.EncodeToString(raw[:])
 // Hard limit log content to a stable set of operation keys.
 if !validOperation(operation) { operation = "unknown" }
 log.Printf("diagnostic_id=%s operation=%s category=%s", id, operation, Classify(err))
 return id
}

var operationPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,79}$`)
func validOperation(operation string) bool { return operationPattern.MatchString(operation) }

func ClientMessage(message, id string) string {
 return fmt.Sprintf("%s (diagnostic ID: %s)", message, id)
}

// Package diagnostics records operational failures without exposing raw internal errors to clients.
package diagnostics

import (
 "crypto/rand"
 "encoding/hex"
 "fmt"
 "log"
 "regexp"
 "strings"
)

var sensitiveValue = regexp.MustCompile(`(?i)(bearer\s+)[^\s,;"']+|((?:access_token|refresh_token|id_token|client_secret|authorization|password|api_key|agent-token|api-token)\s*[=:]\s*["']?)[^\s,;"'}&]+`)
var jwtValue = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)

// Report returns a short correlation ID and writes a redacted diagnostic to server logs.
// Do not pass user message bodies or credential payloads as operation names.
func Report(operation string, err error) string {
 var raw [8]byte
 if _, e := rand.Read(raw[:]); e != nil {
  // IDs are diagnostic only; random read errors do not block user requests.
  raw = [8]byte{}
 }
 id := hex.EncodeToString(raw[:])
 detail := ""
 if err != nil {
  detail = err.Error()
 }
 detail = sensitiveValue.ReplaceAllStringFunc(detail, func(value string) string {
  index := strings.IndexAny(value, "=:")
  if index >= 0 { return value[:index+1] + "[REDACTED]" }
  if strings.HasPrefix(strings.ToLower(value), "bearer ") { return "Bearer [REDACTED]" }
  return "[REDACTED]"
 })
 detail = jwtValue.ReplaceAllString(detail, "[REDACTED_JWT]")
 if len(detail) > 2048 { detail = detail[:2048] + " [TRUNCATED]" }
 log.Printf("diagnostic_id=%s operation=%q cause=%q", id, operation, detail)
 return id
}

// ClientMessage is safe to surface; the raw cause is available in server logs by ID.
func ClientMessage(message, id string) string {
 return fmt.Sprintf("%s (diagnostic ID: %s)", message, id)
}

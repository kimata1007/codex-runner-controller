package logging

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// SanitizeErrorMessage keeps useful upstream error text while excluding
// common credentials and sensitive payload fragments. Redaction is best
// effort: never pass request/response bodies directly to this function.
func SanitizeErrorMessage(message string) string {
	const maxLength = 1024
	message = strings.TrimSpace(message)
	if message == "" {
		return "operation failed"
	}

	// Strip structured fields that may contain arbitrary user input.
	for _, pattern := range sensitiveErrorPatterns {
		message = pattern.ReplaceAllString(message, "${1}[REDACTED]")
	}
	// Strip URL query strings: signed URLs and access tokens can be embedded there.
	message = urlQueryPattern.ReplaceAllString(message, "${1}[QUERY_REDACTED]")
	// Remove header-like / key-value secret assignments.
	message = secretValuePattern.ReplaceAllString(message, "${1}[REDACTED]")
	// Remove bearer/basic credentials and known credential formats.
	message = authSchemePattern.ReplaceAllString(message, "${1}[REDACTED]")
	message = githubTokenPattern.ReplaceAllString(message, "[REDACTED]")
	message = openAITokenPattern.ReplaceAllString(message, "[REDACTED]")
	message = jwtPattern.ReplaceAllString(message, "[REDACTED]")
	message = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 32 {
			return -1
		}
		return r
	}, message)
	message = strings.Join(strings.Fields(message), " ")
	if utf8.RuneCountInString(message) > maxLength {
		runes := []rune(message)
		message = string(runes[:maxLength]) + "…"
	}
	return message
}

var (
	// Entire prompt/body values are removed, rather than attempting to parse them.
	sensitiveErrorPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(\b(?:prompt|user_input|user_message|request_body|response_body|input_text|authorization|cookie|set-cookie|x-api-key|client_secret|refresh_token|access_token|id_token)\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,}]+)`),
		regexp.MustCompile(`(?i)("(?:prompt|input|text|content|messages|authorization|access_token|refresh_token|token|api_key|secret)"\s*:\s*)("(?:\\.|[^"\\])*"|\[[^]]*\]|\{[^}]*\}|[^,}]+)`),
	}
	urlQueryPattern    = regexp.MustCompile(`(https?://[^\s?]+)\?[^\s"'<>]+`)
	secretValuePattern = regexp.MustCompile(`(?i)(\b(?:api[_-]?key|token|password|passwd|secret|access[_-]?token|refresh[_-]?token|client[_-]?secret|authorization|auth[_-]?token)\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,}]+)`)
	authSchemePattern  = regexp.MustCompile(`(?i)(\b(?:Bearer|Basic)\s+)[A-Za-z0-9._~+/-]+=*`)
	githubTokenPattern = regexp.MustCompile(`\b(?:ghp_|gho_|ghu_|ghs_|ghr_|github_pat_)[A-Za-z0-9_]+\b`)
	openAITokenPattern = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{12,}\b`)
	jwtPattern         = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
)

// SanitizeHTTPPath retains resource identifiers for correlating requests.
// It does not return queries, fragments, or credentials, and masks path
// segments which look like opaque authentication tokens.
func SanitizeHTTPPath(path string) string {
	if len(path) == 0 || len(path) > 1024 || !strings.HasPrefix(path, "/") {
		return ""
	}
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		if segment == "" {
			continue
		}
		if len(segment) > 160 || strings.ContainsAny(segment, " \r\n\t?#@") ||
			githubTokenPattern.MatchString(segment) || openAITokenPattern.MatchString(segment) ||
			jwtPattern.MatchString(segment) {
			segments[i] = "[REDACTED]"
		}
	}
	return strings.Join(segments, "/")
}

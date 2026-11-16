package diagnostics

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
)

func TestClassifyNeverReturnsSensitiveErrorText(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{errors.New("authorization: Bearer secret-abc refresh_token=supersecret"), "internal_error"},
		{errors.New("runner-agent returned 502 Bad Gateway: access_token=secret"), "http_502"},
		{errors.New("jsonrpc -32602: user input secret text"), "jsonrpc_-32602"},
		{context.DeadlineExceeded, "deadline_exceeded"},
	}
	for _, tc := range tests {
		got := Classify(tc.err)
		if got != tc.want {
			t.Errorf("classify(%q)=%q want %q", tc.err, got, tc.want)
		}
		for _, secret := range []string{"secret-abc", "supersecret", "secret text"} {
			if strings.Contains(got, secret) {
				t.Errorf("classification contains a secret: %q", got)
			}
		}
	}
}

func TestReportDoesNotLogRawSecrets(t *testing.T) {
	var output bytes.Buffer
	original := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(original)
	id := Report("message_dispatch", errors.New("refresh_token=very-private password=p@ss Bearer jwt.secret text"))
	if len(id) != 24 {
		t.Fatalf("diagnostic ID length = %d", len(id))
	}
	got := output.String()
	if !strings.Contains(got, "diagnostic_id="+id) || !strings.Contains(got, "operation=message_dispatch") {
		t.Fatalf("diagnostic fields missing: %q", got)
	}
	for _, private := range []string{"very-private", "p@ss", "jwt.secret", "text", "password", "refresh_token"} {
		if strings.Contains(got, private) {
			t.Fatalf("log leaked sensitive data: %q", got)
		}
	}
}

func TestOperationNameMustBeAllowlisted(t *testing.T) {
	var output bytes.Buffer
	original := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(original)
	Report("token=supersecret", errors.New("bad"))
	if strings.Contains(output.String(), "supersecret") {
		t.Fatal("untrusted operation was logged")
	}
	if !strings.Contains(output.String(), "operation=unknown") {
		t.Fatal("expected unknown operation")
	}
}

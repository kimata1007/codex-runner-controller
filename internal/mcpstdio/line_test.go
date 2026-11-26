package mcpstdio

import (
	"bufio"
	"errors"
	"strings"
	"testing"
)

func TestReadLineBoundsInputMessages(t *testing.T) {
	if _, err := ReadLine(bufio.NewReader(strings.NewReader(strings.Repeat("x", MaxMessageBytes+1) + "\n"))); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("oversized line error = %v", err)
	}
	got, err := ReadLine(bufio.NewReader(strings.NewReader("{}\n")))
	if err != nil || string(got) != "{}\n" {
		t.Fatalf("line = %q, error = %v", got, err)
	}
}

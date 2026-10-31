package db

import (
	"strings"
	"testing"
)

func TestInitialSchemaContainsCoreTables(t *testing.T) {
	for _, table := range []string{"conversations", "messages", "runs", "events", "approvals"} {
		if !strings.Contains(InitialSchema, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("schema does not define %s", table)
		}
	}
}

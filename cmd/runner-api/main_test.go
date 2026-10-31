package main

import (
	"testing"
)

func TestRepositoryFromEnvironmentDefaultsToMemory(t *testing.T) {
	t.Setenv("LIBSQL_URL", "")

	repository, migrate, err := repositoryFromEnvironment()
	if err != nil {
		t.Fatalf("repositoryFromEnvironment: %v", err)
	}
	if migrate != nil {
		t.Fatal("migrate should be nil for memory repository")
	}
	if _, err := repository.CreateConversation("chat"); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
}

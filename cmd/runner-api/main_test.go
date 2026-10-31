package main

import (
	"context"
	"testing"
)

func TestRepositoryFromEnvironmentDefaultsToMemory(t *testing.T) {
	t.Setenv("LIBSQL_URL", "")

	repository, err := repositoryFromEnvironment(context.Background())
	if err != nil {
		t.Fatalf("repositoryFromEnvironment: %v", err)
	}
	if _, err := repository.CreateConversation(context.Background()); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
}

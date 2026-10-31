package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/kimata1007/codex-runner-controller/internal/agent"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	command := os.Getenv("CODEX_APP_SERVER_COMMAND")
	if command == "" {
		command = "codex"
	}
	args := strings.Fields(os.Getenv("CODEX_APP_SERVER_ARGS"))
	if len(args) == 0 {
		args = []string{"app-server", "--listen", "stdio://"}
	}

	process, err := agent.StartProcess(ctx, command, args, func(message agent.Message) {
		log.Printf("app-server event: %s", message.Method)
	})
	if err != nil {
		log.Fatal(err)
	}
	defer process.Close()

	var initializeResult map[string]any
	if err := process.Client.Call(ctx, "initialize", map[string]any{
		"clientInfo": map[string]string{
			"name":    "codex-runner-controller",
			"title":   "Codex Runner Controller",
			"version": "0.1.0",
		},
	}, &initializeResult); err != nil {
		log.Fatal(err)
	}
	if err := process.Client.Notify("initialized", map[string]any{}); err != nil {
		log.Fatal(err)
	}
	if err := process.Wait(); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}

package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestDefaultAppServerArgsUseChatGPTTokenProvider(t *testing.T) {
	t.Setenv("ACCESS_TOKEN", "present-only-in-test")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	args := defaultAppServerArgs()
	contains := func(want []string) bool {
		for position := 0; position+len(want) <= len(args); position++ {
			if reflect.DeepEqual(args[position:position+len(want)], want) {
				return true
			}
		}
		return false
	}
	if !contains([]string{"-c", `model_provider="openai_chatgpt_plan"`}) || !contains([]string{"-c", `model_providers.openai_chatgpt_plan.env_key="ACCESS_TOKEN"`}) {
		t.Fatalf("args = %v, missing provider configuration", args)
	}
}

func TestDefaultAppServerArgsStayMinimalWithoutToken(t *testing.T) {
	t.Setenv("ACCESS_TOKEN", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	if got := defaultAppServerArgs(); !reflect.DeepEqual(got, []string{"-c", `sandbox_mode="danger-full-access"`, "app-server", "--listen", "stdio://"}) {
		t.Fatalf("args = %v", got)
	}
}

func TestDefaultAppServerArgsEnableOnlyCodexTraces(t *testing.T) {
	t.Setenv("ACCESS_TOKEN", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318")
	args := defaultAppServerArgs()
	if len(args) < 8 || args[0] != "-c" || args[2] != "-c" {
		t.Fatalf("missing Codex OTel arguments: %v", args)
	}
	if !strings.Contains(args[1], "http://collector:4318/v1/traces") {
		t.Fatalf("wrong Codex trace endpoint: %v", args)
	}
	if !strings.Contains(args[1], "otlp-http") || !strings.Contains(args[1], "protocol") {
		t.Fatalf("missing Codex OTel exporter configuration: %v", args)
	}
	if args[len(args)-3] != "app-server" {
		t.Fatalf("Codex subcommand changed: %v", args)
	}
}

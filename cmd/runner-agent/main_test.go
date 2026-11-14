package main

import (
	"reflect"
	"testing"
)

func TestDefaultAppServerArgsUseChatGPTTokenProvider(t *testing.T) {
	t.Setenv("ACCESS_TOKEN", "present-only-in-test")
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
	if got := defaultAppServerArgs(); !reflect.DeepEqual(got, []string{"app-server", "--listen", "stdio://"}) {
		t.Fatalf("args = %v", got)
	}
}

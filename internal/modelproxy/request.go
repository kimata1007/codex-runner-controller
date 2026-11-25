package modelproxy

import (
	"encoding/json"
	"errors"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
)

var errInvalidRequest = errors.New("unapproved model request")

func prepare(raw []byte, policy *v1alpha1.ModelPolicy, session types.UID) ([]byte, string, bool, rateCard, error) {
	if policy.BudgetMicros <= 0 || policy.BudgetMicros > 1000000000000 ||
		policy.RequestsPerMinute <= 0 || policy.RequestsPerMinute > 1200 || session == "" {
		return nil, "", false, rateCard{}, errInvalidRequest
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, "", false, rateCard{}, errInvalidRequest
	}
	for name := range fields {
		switch name {
		case "model", "input", "instructions", "text", "reasoning", "include", "tools", "tool_choice",
			"parallel_tool_calls", "max_output_tokens", "store", "stream", "prompt_cache_key", "service_tier", "truncation":
		default:
			return nil, "", false, rateCard{}, errInvalidRequest
		}
	}
	var name string
	var stream bool
	if json.Unmarshal(fields["model"], &name) != nil || name == "" {
		return nil, "", false, rateCard{}, errInvalidRequest
	}
	if value := fields["stream"]; len(value) > 0 && json.Unmarshal(value, &stream) != nil {
		return nil, "", false, rateCard{}, errInvalidRequest
	}
	var grant rateCard
	for _, allowed := range policy.Models {
		if allowed.Name == name {
			grant = rateCard(allowed)
			break
		}
	}
	if grant.Name == "" || grant.MaxInputBytes <= 0 || grant.MaxInputBytes > 1048576 || int64(len(raw)) > grant.MaxInputBytes ||
		grant.MaxOutputTokens <= 0 || grant.MaxOutputTokens > 131072 ||
		grant.InputMicrosPerMillion <= 0 || grant.InputMicrosPerMillion > 1000000000 ||
		grant.OutputMicrosPerMillion <= 0 || grant.OutputMicrosPerMillion > 1000000000 {
		return nil, "", false, rateCard{}, errInvalidRequest
	}
	var input any
	if json.Unmarshal(fields["input"], &input) != nil || input == nil || !localInput(input) {
		return nil, "", false, rateCard{}, errInvalidRequest
	}
	var tools []struct{ Type string }
	if value := fields["tools"]; len(value) > 0 && json.Unmarshal(value, &tools) != nil {
		return nil, "", false, rateCard{}, errInvalidRequest
	}
	for _, tool := range tools {
		if tool.Type != "function" && tool.Type != "custom" {
			return nil, "", false, rateCard{}, errInvalidRequest
		}
	}
	if value := fields["max_output_tokens"]; len(value) > 0 {
		var requestMax int64
		if json.Unmarshal(value, &requestMax) != nil || requestMax <= 0 || requestMax > grant.MaxOutputTokens {
			return nil, "", false, rateCard{}, errInvalidRequest
		}
		grant.MaxOutputTokens = requestMax
	}
	fields["max_output_tokens"], _ = json.Marshal(grant.MaxOutputTokens)
	fields["prompt_cache_key"], _ = json.Marshal(string(session))
	fields["service_tier"] = json.RawMessage(`"default"`)
	fields["store"] = json.RawMessage("false")
	fields["truncation"] = json.RawMessage(`"disabled"`)
	body, err := json.Marshal(fields)
	return body, name, stream, grant, err
}

func localInput(value any) bool {
	switch value := value.(type) {
	case []any:
		for _, item := range value {
			if !localInput(item) {
				return false
			}
		}
	case map[string]any:
		for name, item := range value {
			switch name {
			case "file_id", "file_url", "image_url", "audio_url":
				return false
			case "type":
				switch item {
				case "item_reference", "input_image", "input_audio", "input_file", "computer_screenshot":
					return false
				}
			}
			if !localInput(item) {
				return false
			}
		}
	}
	return true
}

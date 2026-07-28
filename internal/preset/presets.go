package preset

import "github.com/yeck/easy-llm-router/internal/domain"

const (
	OpenCodeGoID  = "opencode-go"
	OpenCodeZenID = "opencode-zen"
	OpenAIID      = "openai-compatible"
	AnthropicID   = "anthropic-compatible"
)

func Builtins() []domain.Service {
	return []domain.Service{
		openCode(OpenCodeGoID, "OpenCode Go", "https://opencode.ai/zen/go/v1", "GoUsageLimitError"),
		openCode(OpenCodeZenID, "OpenCode Zen", "https://opencode.ai/zen/v1", "FreeUsageLimitError"),
		{
			ID:         OpenAIID,
			Name:       "OpenAI-compatible",
			Preset:     OpenAIID,
			BaseURL:    "https://api.openai.com/v1",
			AuthHeader: "Authorization",
			AuthPrefix: "Bearer ",
			Rules:      authenticationRules(),
			Probe: domain.ProbeConfig{
				Protocol: domain.ProbeOpenAIChat,
				Model:    "gpt-4o-mini",
			},
		},
		{
			ID:         AnthropicID,
			Name:       "Anthropic-compatible",
			Preset:     AnthropicID,
			BaseURL:    "https://api.anthropic.com/v1",
			AuthHeader: "x-api-key",
			Rules: append([]domain.ResponseRule{
				jsonTypeRule("anthropic-authentication-error", domain.ClassInvalid, "error.type", "authentication_error"),
			}, authenticationRules()...),
			Probe: domain.ProbeConfig{
				Protocol: domain.ProbeAnthropic,
				Model:    "claude-haiku-4-5",
				Headers:  map[string]string{"anthropic-version": "2023-06-01"},
			},
		},
	}
}

func openCode(id, name, baseURL, quotaType string) domain.Service {
	return domain.Service{
		ID:         id,
		Name:       name,
		Preset:     id,
		BaseURL:    baseURL,
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
		Rules: append([]domain.ResponseRule{
			jsonTypeRule("quota-root-type", domain.ClassExhausted, "type", quotaType),
			jsonTypeRule("quota-error-type", domain.ClassExhausted, "error.type", quotaType),
		}, authenticationRules()...),
		Probe: domain.ProbeConfig{
			Protocol: domain.ProbeOpenAIChat,
			Model:    "glm-5",
		},
	}
}

func authenticationRules() []domain.ResponseRule {
	return []domain.ResponseRule{{
		Name:   "http-authentication-failed",
		Result: domain.ClassInvalid,
		Conditions: []domain.Condition{{
			StatusMin: 401,
			StatusMax: 401,
		}},
	}}
}

func jsonTypeRule(name string, result domain.Classification, path, value string) domain.ResponseRule {
	return domain.ResponseRule{
		Name:   name,
		Result: result,
		Conditions: []domain.Condition{{
			JSONPath: path,
			Operator: domain.MatchEquals,
			Value:    value,
		}},
	}
}

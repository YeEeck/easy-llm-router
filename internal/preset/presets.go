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
		withOpenCodeHints(openCode(OpenCodeGoID, "OpenCode Go", "https://opencode.ai/zen/go/v1", "GoUsageLimitError")),
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

func withOpenCodeHints(service domain.Service) domain.Service {
	service.RecoveryHint = domain.RecoveryHintConfig{
		Source: domain.RecoveryHintFromHeader,
		Header: "Retry-After",
		Parse:  domain.RecoveryHintAsSeconds,
	}
	service.QuotaEpoch = domain.QuotaEpochConfig{
		Source:   domain.RecoveryHintFromJSON,
		JSONPath: "metadata.limitName",
	}
	return service
}

// ApplyServiceDefaults fills in RecoveryHint and QuotaEpoch for services that
// reference a known preset by ID and have not customized those fields. Other
// fields (model, rules, auth, base URL) are preserved as the user set them.
// Services without a preset reference, or referencing an unknown preset, are
// left untouched so custom services stay verbatim.
func ApplyServiceDefaults(service *domain.Service) {
	if service.Preset == "" {
		return
	}
	for _, builtin := range Builtins() {
		if builtin.ID != service.Preset {
			continue
		}
		if service.RecoveryHint.Source == "" && service.RecoveryHint.Parse == "" {
			service.RecoveryHint = builtin.RecoveryHint
		}
		if service.QuotaEpoch.Source == "" {
			service.QuotaEpoch = builtin.QuotaEpoch
		}
		return
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

package preset

import (
	"testing"

	"github.com/yeck/easy-llm-router/internal/classify"
	"github.com/yeck/easy-llm-router/internal/domain"
)

func TestOpenCodeRulesAreConservative(t *testing.T) {
	service := Builtins()[0]
	quota := classify.Evaluate(service.Rules, classify.Response{StatusCode: 429, Body: []byte(`{"error":{"type":"GoUsageLimitError"}}`)})
	if quota.Result != domain.ClassExhausted {
		t.Fatalf("quota classified as %q", quota.Result)
	}
	providerLimit := classify.Evaluate(service.Rules, classify.Response{StatusCode: 429, Body: []byte(`{"error":{"code":"provider_rate_limit_exceeded"}}`)})
	if providerLimit.Result != domain.ClassInconclusive {
		t.Fatalf("provider limit classified as %q", providerLimit.Result)
	}
}

func TestApplyServiceDefaultsBackfillsRecoveryHintAndQuotaEpochForKnownPreset(t *testing.T) {
	existing := domain.Service{
		ID:      "opencode-go",
		Preset:  OpenCodeGoID,
		BaseURL: "https://opencode.ai/zen/go/v1",
		Probe:   domain.ProbeConfig{Protocol: domain.ProbeOpenAIChat, Model: "deepseek-v4-flash"},
	}
	ApplyServiceDefaults(&existing)
	if existing.RecoveryHint.Source == "" {
		t.Fatalf("recovery hint was not backfilled for preset %q", OpenCodeGoID)
	}
	if existing.QuotaEpoch.Source == "" {
		t.Fatalf("quota epoch was not backfilled for preset %q", OpenCodeGoID)
	}
	if existing.Probe.Model != "deepseek-v4-flash" {
		t.Fatalf("preset backfill clobbered user-customized probe model: %q", existing.Probe.Model)
	}
}

func TestApplyServiceDefaultsPreservesCustomizedRecoveryHint(t *testing.T) {
	custom := domain.RecoveryHintConfig{Source: domain.RecoveryHintFromHeader, Header: "X-Recovery", Parse: domain.RecoveryHintAsSeconds}
	existing := domain.Service{
		ID:           "opencode-go",
		Preset:       OpenCodeGoID,
		RecoveryHint: custom,
	}
	ApplyServiceDefaults(&existing)
	if existing.RecoveryHint.Source != domain.RecoveryHintFromHeader || existing.RecoveryHint.Header != "X-Recovery" {
		t.Fatalf("preset backfill overwrote user-customized recovery hint: %#v", existing.RecoveryHint)
	}
	if existing.QuotaEpoch.Source == "" {
		t.Fatalf("preset backfill did not fill quota epoch when only recovery hint was customized")
	}
}

func TestApplyServiceDefaultsLeavesCustomServicesUntouched(t *testing.T) {
	custom := domain.Service{
		ID:      "custom",
		Preset:  "",
		BaseURL: "https://example.com/v1",
	}
	ApplyServiceDefaults(&custom)
	if custom.RecoveryHint.Source != "" || custom.QuotaEpoch.Source != "" {
		t.Fatalf("custom service was modified by preset backfill: %#v", custom)
	}
}

func TestApplyServiceDefaultsLeavesUnknownPresetUntouched(t *testing.T) {
	existing := domain.Service{
		ID:     "self-hosted",
		Preset: "unknown-preset",
	}
	ApplyServiceDefaults(&existing)
	if existing.RecoveryHint.Source != "" || existing.QuotaEpoch.Source != "" {
		t.Fatalf("unknown preset service was modified by preset backfill: %#v", existing)
	}
}

func TestApplyServiceDefaultsUpgradesLegacyRecoveryHintAndQuotaEpoch(t *testing.T) {
	legacy := domain.Service{
		ID:      "opencode-go",
		Preset:  OpenCodeGoID,
		BaseURL: "https://opencode.ai/zen/go/v1",
		RecoveryHint: domain.RecoveryHintConfig{
			Source:  domain.RecoveryHintFromBody,
			Pattern: `Resets in (\d+m)`,
			Parse:   domain.RecoveryHintAsDuration,
		},
		QuotaEpoch: domain.QuotaEpochConfig{
			Source:  domain.RecoveryHintFromBody,
			Pattern: `(\d+-hour|weekly|month) usage limit`,
		},
	}
	ApplyServiceDefaults(&legacy)
	builtin := withOpenCodeHints(openCode(OpenCodeGoID, "OpenCode Go", "https://opencode.ai/zen/go/v1", "GoUsageLimitError"))
	if legacy.RecoveryHint != builtin.RecoveryHint {
		t.Fatalf("legacy recovery hint was not upgraded: %#v", legacy.RecoveryHint)
	}
	if legacy.QuotaEpoch != builtin.QuotaEpoch {
		t.Fatalf("legacy quota epoch was not upgraded: %#v", legacy.QuotaEpoch)
	}
	if legacy.BaseURL != "https://opencode.ai/zen/go/v1" {
		t.Fatalf("upgrade clobbered base URL: %#v", legacy.BaseURL)
	}
}

func TestApplyServiceDefaultsUpgradesLegacyEvenWhenOtherCustomizationPresent(t *testing.T) {
	legacy := domain.Service{
		ID:      "opencode-go",
		Preset:  OpenCodeGoID,
		BaseURL: "https://opencode.ai/zen/go/v1",
		Probe:   domain.ProbeConfig{Protocol: domain.ProbeOpenAIChat, Model: "deepseek-v4-flash"},
		RecoveryHint: domain.RecoveryHintConfig{
			Source:  domain.RecoveryHintFromBody,
			Pattern: `Resets in (\d+m)`,
			Parse:   domain.RecoveryHintAsDuration,
		},
	}
	ApplyServiceDefaults(&legacy)
	builtin := withOpenCodeHints(openCode(OpenCodeGoID, "OpenCode Go", "https://opencode.ai/zen/go/v1", "GoUsageLimitError"))
	if legacy.RecoveryHint != builtin.RecoveryHint {
		t.Fatalf("legacy recovery hint not upgraded in presence of other customization: %#v", legacy.RecoveryHint)
	}
	if legacy.Probe.Model != "deepseek-v4-flash" {
		t.Fatalf("upgrade clobbered user-customized probe model: %#v", legacy.Probe.Model)
	}
}

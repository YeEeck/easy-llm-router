package classify

import (
	"net/http"
	"testing"
	"time"

	"github.com/yeck/easy-llm-router/internal/domain"
)

func TestExtractRecoveryBodyDurationFromOpenCodeGoSample(t *testing.T) {
	body := []byte(`{"type":"GoUsageLimitError","message":"5-hour usage limit reached. Resets in 35min."}`)
	cfg := domain.RecoveryHintConfig{
		Source:  domain.RecoveryHintFromBody,
		Pattern: `Resets in (\d+m)`,
		Parse:   domain.RecoveryHintAsDuration,
	}
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	got, ok := ExtractRecovery(cfg, Response{StatusCode: 429, Body: body}, now)
	if !ok {
		t.Fatalf("expected hint, got none")
	}
	want := now.Add(35 * time.Minute)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestExtractRecoveryHeaderSecondsFromOpenCodeGoSample(t *testing.T) {
	cfg := domain.RecoveryHintConfig{
		Source: domain.RecoveryHintFromHeader,
		Header: "Retry-After",
		Parse:  domain.RecoveryHintAsSeconds,
	}
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	headers := http.Header{"Retry-After": {"2094"}}
	got, ok := ExtractRecovery(cfg, Response{StatusCode: 429, Header: headers}, now)
	if !ok {
		t.Fatalf("expected hint, got none")
	}
	want := now.Add(2094 * time.Second)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestExtractQuotaEpochJSONLimitNameFromOpenCodeGoSample(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"GoUsageLimitError","message":"5-hour usage limit reached."},"metadata":{"workspace":"wrk_x","limitName":"5 hour"}}`)
	cfg := domain.QuotaEpochConfig{
		Source:   domain.RecoveryHintFromJSON,
		JSONPath: "metadata.limitName",
	}
	got, ok := ExtractQuotaEpoch(cfg, Response{StatusCode: 429, Body: body})
	if !ok {
		t.Fatalf("expected epoch, got none")
	}
	if got != "5 hour" {
		t.Fatalf("epoch = %q, want \"5 hour\"", got)
	}
}

func TestExtractRecoveryHeaderSeconds(t *testing.T) {
	cfg := domain.RecoveryHintConfig{
		Source: domain.RecoveryHintFromHeader,
		Header: "Retry-After",
		Parse:  domain.RecoveryHintAsSeconds,
	}
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	got, ok := ExtractRecovery(cfg, Response{StatusCode: 429, Header: http.Header{"Retry-After": {"30"}}}, now)
	if !ok {
		t.Fatalf("expected hint, got none")
	}
	want := now.Add(30 * time.Second)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestExtractRecoveryJSONAbsolute(t *testing.T) {
	cfg := domain.RecoveryHintConfig{
		Source:   domain.RecoveryHintFromJSON,
		JSONPath: "resets_at",
		Parse:    domain.RecoveryHintAsAbsolute,
	}
	body := []byte(`{"resets_at":"2026-07-29T12:30:00Z"}`)
	got, ok := ExtractRecovery(cfg, Response{StatusCode: 429, Body: body}, time.Now())
	if !ok {
		t.Fatalf("expected hint, got none")
	}
	want := time.Date(2026, 7, 29, 12, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestExtractRecoveryMissingValue(t *testing.T) {
	cfg := domain.RecoveryHintConfig{
		Source: domain.RecoveryHintFromHeader,
		Header: "Retry-After",
		Parse:  domain.RecoveryHintAsSeconds,
	}
	if _, ok := ExtractRecovery(cfg, Response{StatusCode: 429, Header: http.Header{}}, time.Now()); ok {
		t.Fatalf("expected no hint when header absent")
	}
}

func TestExtractRecoveryPatternNoMatch(t *testing.T) {
	cfg := domain.RecoveryHintConfig{
		Source:  domain.RecoveryHintFromBody,
		Pattern: `Resets in (\d+m)`,
		Parse:   domain.RecoveryHintAsDuration,
	}
	if _, ok := ExtractRecovery(cfg, Response{StatusCode: 429, Body: []byte(`{"error":{"type":"FreeUsageLimitError","message":"Please try again later."}}`)}, time.Now()); ok {
		t.Fatalf("expected no hint when pattern misses")
	}
}

func TestExtractRecoveryUnparseableDuration(t *testing.T) {
	cfg := domain.RecoveryHintConfig{
		Source:  domain.RecoveryHintFromBody,
		Pattern: `Resets in (.*)`,
		Parse:   domain.RecoveryHintAsDuration,
	}
	if _, ok := ExtractRecovery(cfg, Response{StatusCode: 429, Body: []byte(`Resets in soon`)}, time.Now()); ok {
		t.Fatalf("expected no hint when duration cannot be parsed")
	}
}

func TestExtractRecoveryEmptyConfig(t *testing.T) {
	if _, ok := ExtractRecovery(domain.RecoveryHintConfig{}, Response{StatusCode: 429}, time.Now()); ok {
		t.Fatalf("expected no hint when config absent")
	}
}

func TestValidateRecoveryHintAcceptsEmpty(t *testing.T) {
	if err := ValidateRecoveryHint(domain.RecoveryHintConfig{}); err != nil {
		t.Fatalf("empty config should be valid, got %v", err)
	}
}

func TestValidateRecoveryHintRejectsMissingSourceField(t *testing.T) {
	cfg := domain.RecoveryHintConfig{Source: domain.RecoveryHintFromHeader, Parse: domain.RecoveryHintAsSeconds}
	if err := ValidateRecoveryHint(cfg); err == nil {
		t.Fatalf("expected error when header source lacks header name")
	}
	cfg = domain.RecoveryHintConfig{Source: domain.RecoveryHintFromJSON, Parse: domain.RecoveryHintAsSeconds}
	if err := ValidateRecoveryHint(cfg); err == nil {
		t.Fatalf("expected error when json source lacks json_path")
	}
}

func TestValidateRecoveryHintRejectsBadPattern(t *testing.T) {
	cfg := domain.RecoveryHintConfig{Source: domain.RecoveryHintFromBody, Pattern: `(unbalanced`, Parse: domain.RecoveryHintAsDuration}
	if err := ValidateRecoveryHint(cfg); err == nil {
		t.Fatalf("expected error for invalid regex")
	}
}

func TestExtractQuotaEpochBodyPatternFromOpenCodeGoSample(t *testing.T) {
	body := []byte(`{"error":{"type":"GoUsageLimitError","message":"5-hour usage limit reached. Resets in 21min."}}`)
	cfg := domain.QuotaEpochConfig{
		Source:  domain.RecoveryHintFromBody,
		Pattern: `(\d+-hour) usage limit`,
	}
	got, ok := ExtractQuotaEpoch(cfg, Response{StatusCode: 429, Body: body})
	if !ok {
		t.Fatalf("expected epoch, got none")
	}
	if got != "5-hour" {
		t.Fatalf("epoch = %q, want 5-hour", got)
	}
}

func TestExtractQuotaEpochHeader(t *testing.T) {
	cfg := domain.QuotaEpochConfig{Source: domain.RecoveryHintFromHeader, Header: "X-Quota-Epoch"}
	got, ok := ExtractQuotaEpoch(cfg, Response{StatusCode: 429, Header: http.Header{"X-Quota-Epoch": {"weekly"}}})
	if !ok {
		t.Fatalf("expected epoch, got none")
	}
	if got != "weekly" {
		t.Fatalf("epoch = %q, want weekly", got)
	}
}

func TestExtractQuotaEpochPatternNoMatch(t *testing.T) {
	cfg := domain.QuotaEpochConfig{Source: domain.RecoveryHintFromBody, Pattern: `(\d+-hour) usage limit`}
	if _, ok := ExtractQuotaEpoch(cfg, Response{StatusCode: 429, Body: []byte(`{"error":{"type":"FreeUsageLimitError"}}`)}); ok {
		t.Fatalf("expected no epoch when pattern misses")
	}
}

func TestValidateQuotaEpochAcceptsEmpty(t *testing.T) {
	if err := ValidateQuotaEpoch(domain.QuotaEpochConfig{}); err != nil {
		t.Fatalf("empty config should be valid, got %v", err)
	}
}

func TestValidateQuotaEpochRejectsMissingSourceField(t *testing.T) {
	if err := ValidateQuotaEpoch(domain.QuotaEpochConfig{Source: domain.RecoveryHintFromHeader}); err == nil {
		t.Fatalf("expected error when header source lacks header name")
	}
}

func TestValidateQuotaEpochRejectsBadPattern(t *testing.T) {
	if err := ValidateQuotaEpoch(domain.QuotaEpochConfig{Source: domain.RecoveryHintFromBody, Pattern: `(unbalanced`}); err == nil {
		t.Fatalf("expected error for invalid regex")
	}
}

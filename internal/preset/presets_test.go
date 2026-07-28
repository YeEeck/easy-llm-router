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

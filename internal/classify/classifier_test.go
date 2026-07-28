package classify

import (
	"net/http"
	"testing"

	"github.com/yeck/easy-llm-router/internal/domain"
)

func TestEvaluateOrderedRules(t *testing.T) {
	rules := []domain.ResponseRule{
		{Name: "quota", Result: domain.ClassExhausted, Conditions: []domain.Condition{{JSONPath: "error.type", Operator: domain.MatchEquals, Value: "GoUsageLimitError"}}},
		{Name: "auth", Result: domain.ClassInvalid, Conditions: []domain.Condition{{StatusMin: 401, StatusMax: 403}}},
	}
	match := Evaluate(rules, Response{StatusCode: 429, Body: []byte(`{"error":{"type":"GoUsageLimitError"}}`)})
	if match.Result != domain.ClassExhausted || match.RuleName != "quota" {
		t.Fatalf("unexpected match: %#v", match)
	}
	match = Evaluate(rules, Response{StatusCode: 429, Body: []byte(`{"error":{"code":"provider_rate_limit_exceeded"}}`)})
	if match.Result != domain.ClassInconclusive {
		t.Fatalf("provider rate limit classified as %q", match.Result)
	}
	match = Evaluate(rules, Response{StatusCode: 200, Header: http.Header{}})
	if match.Result != domain.ClassSuccess {
		t.Fatalf("2xx classified as %q", match.Result)
	}
}

func TestHeaderRegex(t *testing.T) {
	rules := []domain.ResponseRule{{
		Name: "header", Result: domain.ClassExhausted,
		Conditions: []domain.Condition{{Header: "X-Error", Operator: domain.MatchRegex, Value: `(?i)quota`}},
	}}
	match := Evaluate(rules, Response{StatusCode: 429, Header: http.Header{"X-Error": {"Quota exceeded"}}})
	if match.Result != domain.ClassExhausted {
		t.Fatalf("unexpected match: %#v", match)
	}
}

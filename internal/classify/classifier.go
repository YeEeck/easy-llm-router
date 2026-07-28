package classify

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/yeck/easy-llm-router/internal/domain"
)

type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

type Match struct {
	Result   domain.Classification
	RuleName string
}

func Evaluate(rules []domain.ResponseRule, response Response) Match {
	for _, rule := range rules {
		if matchesRule(rule, response) {
			return Match{Result: rule.Result, RuleName: rule.Name}
		}
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return Match{Result: domain.ClassSuccess, RuleName: "default-2xx"}
	}
	return Match{Result: domain.ClassInconclusive, RuleName: "default-inconclusive"}
}

func ValidateRules(rules []domain.ResponseRule) error {
	for i, rule := range rules {
		if strings.TrimSpace(rule.Name) == "" {
			return fmt.Errorf("rule %d has no name", i+1)
		}
		switch rule.Result {
		case domain.ClassSuccess, domain.ClassExhausted, domain.ClassInvalid, domain.ClassInconclusive:
		default:
			return fmt.Errorf("rule %q has invalid result %q", rule.Name, rule.Result)
		}
		if len(rule.Conditions) == 0 {
			return fmt.Errorf("rule %q has no conditions", rule.Name)
		}
		for _, condition := range rule.Conditions {
			if condition.Operator == domain.MatchRegex {
				if _, err := regexp.Compile(condition.Value); err != nil {
					return fmt.Errorf("rule %q has invalid regular expression: %w", rule.Name, err)
				}
			}
		}
	}
	return nil
}

func matchesRule(rule domain.ResponseRule, response Response) bool {
	if len(rule.Conditions) == 0 {
		return false
	}
	for _, condition := range rule.Conditions {
		if !matchesCondition(condition, response) {
			return false
		}
	}
	return true
}

func matchesCondition(condition domain.Condition, response Response) bool {
	if condition.StatusMin != 0 || condition.StatusMax != 0 {
		maxStatus := condition.StatusMax
		if maxStatus == 0 {
			maxStatus = condition.StatusMin
		}
		if response.StatusCode < condition.StatusMin || response.StatusCode > maxStatus {
			return false
		}
	}
	if condition.Header != "" {
		values, exists := response.Header[http.CanonicalHeaderKey(condition.Header)]
		if !matchValue(strings.Join(values, ","), exists, condition) {
			return false
		}
	}
	if condition.JSONPath != "" {
		value, exists := jsonValue(response.Body, condition.JSONPath)
		if !matchValue(value, exists, condition) {
			return false
		}
	}
	if condition.Body {
		if !matchValue(string(response.Body), len(response.Body) > 0, condition) {
			return false
		}
	}
	return true
}

func jsonValue(body []byte, path string) (string, bool) {
	var current any
	if err := json.Unmarshal(body, &current); err != nil {
		return "", false
	}
	for _, part := range strings.Split(path, ".") {
		switch node := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = node[part]
			if !ok {
				return "", false
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(node) {
				return "", false
			}
			current = node[index]
		default:
			return "", false
		}
	}
	if value, ok := current.(string); ok {
		return value, true
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		return "", false
	}
	return string(encoded), true
}

func matchValue(value string, exists bool, condition domain.Condition) bool {
	switch condition.Operator {
	case "", domain.MatchExists:
		return exists
	case domain.MatchEquals:
		return exists && value == condition.Value
	case domain.MatchRegex:
		matched, err := regexp.MatchString(condition.Value, value)
		return err == nil && exists && matched
	default:
		return false
	}
}

func Describe(response Response, match Match) string {
	return strconv.Itoa(response.StatusCode) + ":" + string(match.Result) + ":" + match.RuleName
}

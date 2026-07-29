package classify

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yeck/easy-llm-router/internal/domain"
)

// ExtractRecovery interprets an upstream response against a service's recovery
// hint config and returns the absolute moment the upstream says it will recover.
// Returns false when no config is set, no value is present, or the value cannot
// be parsed. It never returns an error: an unparseable hint is treated as no
// hint, so a malformed upstream signal cannot block the exhausted transition.
func ExtractRecovery(cfg domain.RecoveryHintConfig, response Response, now time.Time) (time.Time, bool) {
	if cfg.Source == "" || cfg.Parse == "" {
		return time.Time{}, false
	}
	raw, ok := extractHintValue(cfg, response)
	if !ok {
		return time.Time{}, false
	}
	value := raw
	if cfg.Pattern != "" {
		re, err := regexp.Compile(cfg.Pattern)
		if err != nil {
			return time.Time{}, false
		}
		match := re.FindStringSubmatch(raw)
		if match == nil {
			return time.Time{}, false
		}
		if len(match) > 1 {
			value = match[1]
		} else {
			value = match[0]
		}
	}
	switch cfg.Parse {
	case domain.RecoveryHintAsDuration:
		d, err := time.ParseDuration(value)
		if err != nil || d < 0 {
			return time.Time{}, false
		}
		return now.Add(d), true
	case domain.RecoveryHintAsAbsolute:
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return time.Time{}, false
		}
		return parsed, true
	case domain.RecoveryHintAsSeconds:
		seconds, err := strconv.Atoi(value)
		if err != nil || seconds < 0 {
			return time.Time{}, false
		}
		return now.Add(time.Duration(seconds) * time.Second), true
	}
	return time.Time{}, false
}

func extractHintValue(cfg domain.RecoveryHintConfig, response Response) (string, bool) {
	switch cfg.Source {
	case domain.RecoveryHintFromHeader:
		if cfg.Header == "" {
			return "", false
		}
		values, exists := response.Header[http.CanonicalHeaderKey(cfg.Header)]
		if !exists || len(values) == 0 {
			return "", false
		}
		return strings.Join(values, ","), true
	case domain.RecoveryHintFromJSON:
		if cfg.JSONPath == "" {
			return "", false
		}
		return jsonValue(response.Body, cfg.JSONPath)
	case domain.RecoveryHintFromBody:
		if len(response.Body) == 0 {
			return "", false
		}
		return string(response.Body), true
	}
	return "", false
}

// ValidateRecoveryHint checks that a service's recovery hint config is well
// formed. An empty config (no source, no parse, no fields) is valid and means
// the service does not capture a recovery hint.
func ValidateRecoveryHint(cfg domain.RecoveryHintConfig) error {
	empty := cfg.Source == "" && cfg.Parse == "" && cfg.Header == "" && cfg.JSONPath == "" && cfg.Pattern == ""
	if empty {
		return nil
	}
	switch cfg.Source {
	case domain.RecoveryHintFromHeader:
		if cfg.Header == "" {
			return errors.New("recovery hint source header requires header name")
		}
	case domain.RecoveryHintFromJSON:
		if cfg.JSONPath == "" {
			return errors.New("recovery hint source json requires json_path")
		}
	case domain.RecoveryHintFromBody:
		// body source reads the whole response body, no extra field required
	default:
		return fmt.Errorf("recovery hint source %q is invalid", cfg.Source)
	}
	switch cfg.Parse {
	case domain.RecoveryHintAsDuration, domain.RecoveryHintAsAbsolute, domain.RecoveryHintAsSeconds:
	default:
		return fmt.Errorf("recovery hint parse %q is invalid", cfg.Parse)
	}
	if cfg.Pattern != "" {
		if _, err := regexp.Compile(cfg.Pattern); err != nil {
			return fmt.Errorf("recovery hint pattern: %w", err)
		}
	}
	return nil
}

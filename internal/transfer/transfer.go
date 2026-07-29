package transfer

import (
	"encoding/base64"
	"fmt"
	"unicode"

	"github.com/yeck/easy-llm-router/internal/domain"
	"gopkg.in/yaml.v3"
)

// Bundle is the composite structure serialized into the base64 config package.
type Bundle struct {
	Config  domain.Config  `yaml:"config"`
	Secrets domain.Secrets `yaml:"secrets"`
}

// Encode serializes a Bundle into a single-line base64 string.
func Encode(cfg domain.Config, secrets domain.Secrets) (string, error) {
	bundle := Bundle{Config: cfg, Secrets: secrets}
	data, err := yaml.Marshal(bundle)
	if err != nil {
		return "", fmt.Errorf("marshal bundle: %w", err)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// Decode parses a base64 string (tolerating any whitespace) into a Bundle.
func Decode(input string) (Bundle, error) {
	var bundle Bundle
	cleaned := stripWhitespace(input)
	if cleaned == "" {
		return bundle, fmt.Errorf("empty input")
	}
	data, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return bundle, fmt.Errorf("base64 decode: %w", err)
	}
	if err := yaml.Unmarshal(data, &bundle); err != nil {
		return bundle, fmt.Errorf("parse bundle: %w", err)
	}
	return bundle, nil
}

func stripWhitespace(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if !unicode.IsSpace(r) {
			out = append(out, r)
		}
	}
	return string(out)
}

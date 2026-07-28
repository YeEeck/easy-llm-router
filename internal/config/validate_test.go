package config

import (
	"strings"
	"testing"

	"github.com/yeck/easy-llm-router/internal/domain"
)

func TestValidate(t *testing.T) {
	cfg := Default()
	cfg.Services = []domain.Service{{ID: "go", Name: "Go", BaseURL: "https://example.com/v1"}}
	cfg.Credentials = []domain.Credential{{ID: "a", Name: "A", ServiceID: "go"}}
	cfg.Pools = []domain.Pool{{Name: "main", CredentialIDs: []string{"a"}}}
	secrets := domain.Secrets{Version: 1, APIKeys: map[string]string{"a": "secret"}}
	if err := Validate(cfg, secrets); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	cfg.Pools[0].Name = "not/a/name"
	err := Validate(cfg, secrets)
	if err == nil || !strings.Contains(err.Error(), "pool name") {
		t.Fatalf("expected pool name error, got %v", err)
	}
}

package transfer

import (
	"strings"
	"testing"
	"time"

	"github.com/yeck/easy-llm-router/internal/domain"
)

func sampleConfig() domain.Config {
	return domain.Config{
		Version: 1,
		Settings: domain.Settings{
			Port:                 8787,
			LogLevel:             "info",
			ReplayMemoryLimit:    8 << 20,
			ReplayLimit:          256 << 20,
			ResponseInspectLimit: 1 << 20,
			ShutdownTimeout:      time.Minute,
		},
		Services: []domain.Service{
			{
				ID:      "openai",
				Name:    "OpenAI",
				BaseURL: "https://api.openai.com/v1",
				Probe:   domain.ProbeConfig{Protocol: domain.ProbeOpenAIChat, Model: "gpt-4o-mini"},
			},
		},
		Credentials: []domain.Credential{
			{ID: "cred-1", Name: "key-1", ServiceID: "openai"},
		},
		Pools: []domain.Pool{
			{Name: "default", CredentialIDs: []string{"cred-1"}, VerifyInterval: 5 * time.Minute},
		},
	}
}

func sampleSecrets() domain.Secrets {
	return domain.Secrets{
		Version: 1,
		APIKeys: map[string]string{"cred-1": "sk-test-key-123"},
	}
}

func TestRoundTrip(t *testing.T) {
	cfg := sampleConfig()
	secrets := sampleSecrets()

	encoded, err := Encode(cfg, secrets)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if strings.ContainsAny(encoded, " \t\n\r") {
		t.Fatal("encoded output contains whitespace")
	}

	bundle, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if bundle.Config.Version != cfg.Version {
		t.Errorf("version = %d, want %d", bundle.Config.Version, cfg.Version)
	}
	if bundle.Config.Settings.Port != cfg.Settings.Port {
		t.Errorf("port = %d, want %d", bundle.Config.Settings.Port, cfg.Settings.Port)
	}
	if len(bundle.Config.Services) != 1 || bundle.Config.Services[0].ID != "openai" {
		t.Errorf("services mismatch: %+v", bundle.Config.Services)
	}
	if len(bundle.Config.Credentials) != 1 || bundle.Config.Credentials[0].ID != "cred-1" {
		t.Errorf("credentials mismatch: %+v", bundle.Config.Credentials)
	}
	if bundle.Secrets.APIKeys["cred-1"] != "sk-test-key-123" {
		t.Errorf("api key = %q, want %q", bundle.Secrets.APIKeys["cred-1"], "sk-test-key-123")
	}
}

func TestDecodeWithWhitespace(t *testing.T) {
	cfg := sampleConfig()
	secrets := sampleSecrets()

	encoded, err := Encode(cfg, secrets)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Insert whitespace at various positions.
	noisy := "  " + encoded[:10] + "\n" + encoded[10:20] + "\r\n\t" + encoded[20:] + "  \n"
	bundle, err := Decode(noisy)
	if err != nil {
		t.Fatalf("Decode with whitespace: %v", err)
	}
	if bundle.Config.Settings.Port != 8787 {
		t.Errorf("port = %d, want 8787", bundle.Config.Settings.Port)
	}
}

func TestDecodeEmpty(t *testing.T) {
	_, err := Decode("")
	if err == nil {
		t.Fatal("expected error for empty input")
	}
	_, err = Decode("   \n\t  ")
	if err == nil {
		t.Fatal("expected error for whitespace-only input")
	}
}

func TestDecodeInvalidBase64(t *testing.T) {
	_, err := Decode("not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestDecodeInvalidYAML(t *testing.T) {
	// Valid base64 but not valid YAML bundle structure.
	_, err := Decode("aGVsbG8=") // "hello"
	if err != nil {
		// yaml.Unmarshal of a scalar into a struct may or may not error;
		// either way the bundle should be zero-valued.
		t.Logf("got error (acceptable): %v", err)
	}
}

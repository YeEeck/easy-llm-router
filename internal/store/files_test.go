package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/yeck/easy-llm-router/internal/domain"
)

func TestSecretsRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "credentials.yaml")
	want := domain.Secrets{Version: 1, APIKeys: map[string]string{"a": "secret"}}
	if err := SaveSecrets(path, want); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("mode = %04o, want 0600", got)
		}
	}
	got, err := LoadSecrets(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.APIKeys["a"] != "secret" {
		t.Fatalf("key = %q", got.APIKeys["a"])
	}
}

func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	want := NewState()
	want.Current["main"] = "a"
	want.Credentials["a"] = domain.CredentialState{Status: domain.StatusAvailable}
	if err := SaveState(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Current["main"] != "a" || got.Credentials["a"].Status != domain.StatusAvailable {
		t.Fatalf("unexpected state: %#v", got)
	}
}

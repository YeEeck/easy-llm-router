package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/yeck/easy-llm-router/internal/domain"
	"gopkg.in/yaml.v3"
)

func LoadConfig(path string) (domain.Config, error) {
	var cfg domain.Config
	if err := loadYAML(path, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func SaveConfig(path string, cfg domain.Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return writeAtomic(path, data, 0o644)
}

func LoadSecrets(path string) (domain.Secrets, error) {
	var secrets domain.Secrets
	info, err := os.Stat(path)
	if err != nil {
		return secrets, err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		return secrets, fmt.Errorf("credentials file %s must have mode 0600, got %04o", path, info.Mode().Perm())
	}
	if err := loadYAML(path, &secrets); err != nil {
		return secrets, err
	}
	if secrets.APIKeys == nil {
		secrets.APIKeys = map[string]string{}
	}
	return secrets, nil
}

func SaveSecrets(path string, secrets domain.Secrets) error {
	data, err := yaml.Marshal(secrets)
	if err != nil {
		return err
	}
	return writeAtomic(path, data, 0o600)
}

func LoadState(path string) (domain.RuntimeState, error) {
	var state domain.RuntimeState
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	normalizeState(&state)
	return state, nil
}

func SaveState(path string, state domain.RuntimeState) error {
	normalizeState(&state)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAtomic(path, data, 0o600)
}

func NewState() domain.RuntimeState {
	state := domain.RuntimeState{}
	normalizeState(&state)
	return state
}

func loadYAML(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, target); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func writeAtomic(path string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, path); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func normalizeState(state *domain.RuntimeState) {
	if state.Version == 0 {
		state.Version = 1
	}
	if state.Credentials == nil {
		state.Credentials = map[string]domain.CredentialState{}
	}
	if state.Current == nil {
		state.Current = map[string]string{}
	}
}

func IsNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}

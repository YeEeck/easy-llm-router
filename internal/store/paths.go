package store

import (
	"os"
	"path/filepath"
)

type Paths struct {
	Config      string
	Credentials string
	State       string
	Log         string
	Temp        string
}

func DefaultPaths() (Paths, error) {
	configRoot, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, err
	}
	stateRoot := os.Getenv("XDG_STATE_HOME")
	if stateRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		stateRoot = filepath.Join(home, ".local", "state")
	}
	configDir := filepath.Join(configRoot, "easy-llm-router")
	stateDir := filepath.Join(stateRoot, "easy-llm-router")
	return Paths{
		Config:      filepath.Join(configDir, "config.yaml"),
		Credentials: filepath.Join(configDir, "credentials.yaml"),
		State:       filepath.Join(stateDir, "state.json"),
		Log:         filepath.Join(stateDir, "router.jsonl"),
		Temp:        filepath.Join(stateDir, "tmp"),
	}, nil
}

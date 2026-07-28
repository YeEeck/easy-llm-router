package store

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestDefaultPathsUsesPlatformStateRoot(t *testing.T) {
	stateRoot := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", t.TempDir())
		t.Setenv("LOCALAPPDATA", stateRoot)
		t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "ignored"))
	} else {
		t.Setenv("XDG_STATE_HOME", stateRoot)
	}

	paths, err := DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(stateRoot, "easy-llm-router")
	if paths.State != filepath.Join(stateDir, "state.json") {
		t.Fatalf("state path = %q", paths.State)
	}
	if paths.Log != filepath.Join(stateDir, "router.jsonl") {
		t.Fatalf("log path = %q", paths.Log)
	}
	if paths.Temp != filepath.Join(stateDir, "tmp") {
		t.Fatalf("temp path = %q", paths.Temp)
	}
}

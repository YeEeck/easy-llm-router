package tui

import (
	"testing"

	"github.com/yeck/easy-llm-router/internal/config"
)

func TestWizardResultIsValid(t *testing.T) {
	wizard := NewWizard()
	wizard.completed = true
	wizard.inputs[2].SetValue("secret")
	cfg, secrets, ok := wizard.Result()
	if !ok {
		t.Fatal("expected completed wizard")
	}
	if err := config.Validate(cfg, secrets); err != nil {
		t.Fatalf("wizard produced invalid config: %v", err)
	}
}

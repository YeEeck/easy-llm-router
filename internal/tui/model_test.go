package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/yeck/easy-llm-router/internal/config"
	"github.com/yeck/easy-llm-router/internal/domain"
	"github.com/yeck/easy-llm-router/internal/logging"
	"github.com/yeck/easy-llm-router/internal/preset"
	"github.com/yeck/easy-llm-router/internal/routing"
	"github.com/yeck/easy-llm-router/internal/store"
)

func TestCredentialEditorLoadsAndRetainsAPIKey(t *testing.T) {
	model := credentialEditorModel(t)
	updated, _ := model.poolAction("e")
	model = updated.(Model)

	if got := model.form.inputs[2].Value(); got != " existing-secret " {
		t.Fatalf("API key value = %q, want existing secret", got)
	}
	if got := model.form.inputs[2].EchoMode; got != textinput.EchoNormal {
		t.Fatalf("API key echo mode = %v, want EchoNormal", got)
	}

	model.form.inputs[0].SetValue("renamed")
	if err := model.submitForm(); err != nil {
		t.Fatalf("submit credential edit: %v", err)
	}
	if got := model.backend.Manager.Secrets().APIKeys["credential-1"]; got != " existing-secret " {
		t.Fatalf("saved API key = %q, want existing secret", got)
	}
	cfg, _ := model.backend.Manager.Snapshot()
	if got := cfg.Credentials[0].Name; got != "renamed" {
		t.Fatalf("credential name = %q, want renamed", got)
	}
}

func TestHelpWrapsWithoutSplittingBindings(t *testing.T) {
	const width = 24
	help := renderHelp(width,
		helpBinding{"[j/k]", "select"},
		helpBinding{"[V]", "verify all"},
		helpBinding{"[n]", "rename/interval"},
	)

	plain := ansi.Strip(help)
	for _, binding := range []string{"[j/k] select", "[V] verify all", "[n] rename/interval"} {
		if !strings.Contains(plain, binding) {
			t.Errorf("help %q does not contain binding %q", plain, binding)
		}
	}
	for _, line := range strings.Split(help, "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Errorf("line width = %d, want <= %d: %q", got, width, ansi.Strip(line))
		}
	}
}

func TestPoolHelpWrapsAtNarrowWidth(t *testing.T) {
	model := Model{screen: screenPools, width: 44}
	help := model.help()
	if lipgloss.Height(help) < 2 {
		t.Fatalf("help height = %d, want wrapped output", lipgloss.Height(help))
	}
	if !strings.Contains(ansi.Strip(help), "[V] verify all") {
		t.Fatalf("wrapped help omitted verify-all binding: %q", ansi.Strip(help))
	}
	for _, line := range strings.Split(help, "\n") {
		if got := lipgloss.Width(line); got > contentWidth(model.width) {
			t.Errorf("line width = %d, want <= %d: %q", got, contentWidth(model.width), ansi.Strip(line))
		}
	}
}

func TestOpenFormRespondsToWindowResize(t *testing.T) {
	model := Model{width: 100, height: 30}
	model.openForm("filter", []string{"Contains"}, []string{""}, -1)

	updated, _ := model.updateForm(tea.WindowSizeMsg{Width: 48, Height: 20})
	model = updated.(Model)
	if model.width != 48 || model.height != 20 {
		t.Fatalf("window size = %dx%d, want 48x20", model.width, model.height)
	}
	if got := model.form.inputs[0].Width(); got != inputWidth(48, 30) {
		t.Fatalf("input width = %d, want %d", got, inputWidth(48, 30))
	}
}

func TestWizardAPIKeyIsVisible(t *testing.T) {
	wizard := NewWizard()
	if got := wizard.inputs[2].EchoMode; got != textinput.EchoNormal {
		t.Fatalf("API key echo mode = %v, want EchoNormal", got)
	}
}

func TestLogViewWrapsLongEntries(t *testing.T) {
	entry := `{"level":"INFO","msg":"` + strings.Repeat("x", 120) + `"}`
	model := logViewModel(t, 46, 30, entry)

	lines := strings.Split(model.viewLogs(), "\n")
	if len(lines) < 3 {
		t.Fatalf("wrapped lines = %d, want >= 3", len(lines))
	}
	if lines[0][0] != '{' {
		t.Errorf("first line starts with %q, want entry head at column 0", lines[0][:1])
	}
	var rebuilt strings.Builder
	rebuilt.WriteString(lines[0])
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, "  ") {
			t.Errorf("continuation line not indented: %q", line)
		}
		rebuilt.WriteString(strings.TrimPrefix(line, "  "))
	}
	if rebuilt.String() != entry {
		t.Errorf("wrapped content differs from entry:\n got %q\nwant %q", rebuilt.String(), entry)
	}
	for _, line := range lines {
		if got := lipgloss.Width(line); got > 40 {
			t.Errorf("line width = %d, want <= 40: %q", got, line)
		}
	}
}

func TestLogViewFillsBudgetFromNewest(t *testing.T) {
	entries := []string{"entry-0", "entry-1", "entry-2", "entry-3", "entry-4", "entry-5", "entry-6"}
	model := logViewModel(t, 60, 14, entries...)

	got := strings.Split(model.viewLogs(), "\n")
	want := entries[2:] // budget = max(5, 14-9) = 5 newest entries
	if len(got) != len(want) {
		t.Fatalf("visible lines = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLogViewTruncatesOldestEntryHead(t *testing.T) {
	entry := strings.Repeat("0123456789", 20)
	model := logViewModel(t, 46, 14, entry)

	lines := strings.Split(model.viewLogs(), "\n")
	const budget = 5 // max(5, 14-9)
	if len(lines) != budget {
		t.Fatalf("visible lines = %d, want budget %d", len(lines), budget)
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "  ") {
			t.Errorf("expected head-truncated continuation line, got %q", line)
		}
	}
	var rebuilt strings.Builder
	for _, line := range lines {
		rebuilt.WriteString(strings.TrimPrefix(line, "  "))
	}
	if !strings.HasSuffix(entry, rebuilt.String()) {
		t.Errorf("visible tail %q is not a suffix of entry", rebuilt.String())
	}
}

func logViewModel(t *testing.T, width, height int, entries ...string) Model {
	t.Helper()
	_, hub, err := logging.New(filepath.Join(t.TempDir(), "log.json"), "info")
	if err != nil {
		t.Fatalf("create log hub: %v", err)
	}
	for _, entry := range entries {
		if _, err := hub.Write([]byte(entry + "\n")); err != nil {
			t.Fatalf("write log entry: %v", err)
		}
	}
	return Model{backend: Backend{Logs: hub}, screen: screenLogs, width: width, height: height}
}

func credentialEditorModel(t *testing.T) Model {
	t.Helper()
	cfg := config.Default()
	cfg.Services = preset.Builtins()
	cfg.Credentials = []domain.Credential{{ID: "credential-1", Name: "primary", ServiceID: preset.OpenCodeGoID}}
	cfg.Pools = []domain.Pool{{Name: "main", CredentialIDs: []string{"credential-1"}}}
	config.ApplyDefaults(&cfg)
	secrets := domain.Secrets{Version: 1, APIKeys: map[string]string{"credential-1": " existing-secret "}}
	manager, err := routing.New(cfg, secrets, store.NewState(), nil)
	if err != nil {
		t.Fatalf("create routing manager: %v", err)
	}
	temp := t.TempDir()
	return NewModel(Backend{
		Manager: manager,
		Paths: store.Paths{
			Config:      filepath.Join(temp, "config.yaml"),
			Credentials: filepath.Join(temp, "credentials.yaml"),
		},
	})
}

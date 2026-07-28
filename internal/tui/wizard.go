package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/yeck/easy-llm-router/internal/config"
	"github.com/yeck/easy-llm-router/internal/domain"
	"github.com/yeck/easy-llm-router/internal/preset"
)

type Wizard struct {
	inputs    []textinput.Model
	focus     int
	completed bool
	err       string
	width     int
}

func NewWizard() Wizard {
	labels := []string{"Pool name", "Credential name", "OpenCode Go API key", "Validation model"}
	defaults := []string{"main", "primary", "", "glm-5"}
	inputs := make([]textinput.Model, len(labels))
	for i := range labels {
		inputs[i] = textinput.New()
		inputs[i].Prompt = ""
		inputs[i].Placeholder = labels[i]
		inputs[i].SetValue(defaults[i])
		inputs[i].SetWidth(48)
	}
	inputs[2].EchoMode = textinput.EchoPassword
	inputs[0].Focus()
	return Wizard{inputs: inputs}
}

func (w Wizard) Init() tea.Cmd { return textinput.Blink }

func (w Wizard) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		w.width = message.Width
		for i := range w.inputs {
			w.inputs[i].SetWidth(max(20, message.Width-32))
		}
	case tea.KeyPressMsg:
		switch message.String() {
		case "ctrl+c", "esc":
			return w, tea.Quit
		case "tab", "down":
			w.moveFocus(1)
			return w, nil
		case "shift+tab", "up":
			w.moveFocus(-1)
			return w, nil
		case "enter":
			if w.focus < len(w.inputs)-1 {
				w.moveFocus(1)
				return w, nil
			}
			if err := w.validate(); err != nil {
				w.err = err.Error()
				return w, nil
			}
			w.completed = true
			return w, tea.Quit
		}
	}
	updated, cmd := w.inputs[w.focus].Update(message)
	w.inputs[w.focus] = updated
	return w, cmd
}

func (w Wizard) View() tea.View {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2")).Render("easy-llm-router setup")
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Width(22)
	labels := []string{"Pool name", "Credential name", "API key", "Validation model"}
	var rows []string
	for i, input := range w.inputs {
		marker := "  "
		if i == w.focus {
			marker = "> "
		}
		rows = append(rows, marker+label.Render(labels[i])+input.View())
	}
	content := title + "\n\n" + muted.Render("Create the first OpenCode Go route. More services can be added later.") + "\n\n" + strings.Join(rows, "\n\n")
	if w.err != "" {
		content += "\n\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render(w.err)
	}
	content += "\n\n" + muted.Render("Tab move  Enter continue  Esc cancel")
	view := tea.NewView(lipgloss.NewStyle().Padding(1, 2).Render(content))
	view.AltScreen = true
	view.WindowTitle = "easy-llm-router setup"
	return view
}

func (w Wizard) Result() (domain.Config, domain.Secrets, bool) {
	if !w.completed {
		return domain.Config{}, domain.Secrets{}, false
	}
	cfg := config.Default()
	cfg.Services = preset.Builtins()
	for i := range cfg.Services {
		if cfg.Services[i].ID == preset.OpenCodeGoID {
			cfg.Services[i].Probe.Model = strings.TrimSpace(w.inputs[3].Value())
		}
	}
	cfg.Credentials = []domain.Credential{{ID: "credential-1", Name: strings.TrimSpace(w.inputs[1].Value()), ServiceID: preset.OpenCodeGoID}}
	cfg.Pools = []domain.Pool{{Name: strings.TrimSpace(w.inputs[0].Value()), CredentialIDs: []string{"credential-1"}}}
	config.ApplyDefaults(&cfg)
	secrets := domain.Secrets{Version: 1, APIKeys: map[string]string{"credential-1": w.inputs[2].Value()}}
	return cfg, secrets, true
}

func (w *Wizard) moveFocus(delta int) {
	w.inputs[w.focus].Blur()
	w.focus = (w.focus + delta + len(w.inputs)) % len(w.inputs)
	w.inputs[w.focus].Focus()
}

func (w Wizard) validate() error {
	for i, input := range w.inputs {
		if strings.TrimSpace(input.Value()) == "" {
			return fmt.Errorf("field %d is required", i+1)
		}
	}
	return nil
}

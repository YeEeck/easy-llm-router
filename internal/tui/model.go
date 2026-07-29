package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/yeck/easy-llm-router/internal/classify"
	"github.com/yeck/easy-llm-router/internal/domain"
)

type screen int

const (
	screenPools screen = iota
	screenServices
	screenLogs
	screenSettings
)

type form struct {
	kind   string
	labels []string
	inputs []textinput.Model
	focus  int
	editID string
}

type validationMsg struct {
	label  string
	result domain.Classification
	err    error
}

type shutdownMsg struct{ err error }
type tickMsg time.Time

type Model struct {
	backend      Backend
	screen       screen
	cursor       int
	poolIndex    int
	serviceIndex int
	width        int
	height       int
	form         *form
	status       string
	logFilter    string
	confirm      string
	shuttingDown bool
	shutdownAt   time.Time
}

func NewModel(backend Backend) Model { return Model{backend: backend} }

func (m Model) Init() tea.Cmd { return tick() }

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if m.form != nil {
		return m.updateForm(message)
	}
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
	case validationMsg:
		if message.err != nil {
			m.status = message.label + ": " + message.err.Error()
		} else {
			m.status = message.label + ": " + string(message.result)
		}
	case shutdownMsg:
		if message.err != nil {
			m.status = "shutdown: " + message.err.Error()
		}
		return m, tea.Quit
	case tickMsg:
		if m.shuttingDown && m.backend.Active() == 0 {
			return m, tea.Quit
		}
		return m, tick()
	case tea.KeyPressMsg:
		key := message.String()
		if m.shuttingDown {
			if key == "q" || key == "ctrl+c" || key == "esc" {
				_ = m.backend.Force()
				return m, tea.Quit
			}
			return m, nil
		}
		switch key {
		case "ctrl+c", "q":
			m.shuttingDown = true
			m.shutdownAt = time.Now()
			return m, m.shutdownCmd()
		case "1":
			m.screen, m.cursor = screenPools, 0
		case "2":
			m.screen, m.cursor = screenServices, 0
		case "3":
			m.screen, m.cursor = screenLogs, 0
		case "4":
			m.screen, m.cursor = screenSettings, 0
		case "j", "down":
			m.moveCursor(1)
		case "k", "up":
			m.moveCursor(-1)
		default:
			return m.handleAction(key)
		}
	}
	return m, nil
}

func (m Model) View() tea.View {
	var content string
	if m.shuttingDown {
		content = fmt.Sprintf("Stopping proxy...\n\n%d active request(s)\n\nPress q again to force exit.", m.backend.Active())
	} else {
		content = m.header() + "\n\n"
		switch m.screen {
		case screenPools:
			content += m.viewPools()
		case screenServices:
			content += m.viewServices()
		case screenLogs:
			content += m.viewLogs()
		case screenSettings:
			content += m.viewSettings()
		}
		if m.status != "" {
			content += "\n\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render(m.status)
		}
		content += "\n\n" + m.help()
	}
	if m.form != nil {
		content = m.viewForm()
	}
	view := tea.NewView(lipgloss.NewStyle().Padding(1, 2).Render(content))
	view.AltScreen = true
	view.WindowTitle = "easy-llm-router"
	return view
}

func (m Model) header() string {
	tabs := []string{"1 Pools", "2 Services", "3 Logs", "4 Settings"}
	for i := range tabs {
		style := lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("8"))
		if int(m.screen) == i {
			style = style.Bold(true).Foreground(lipgloss.Color("2")).Underline(true)
		}
		tabs[i] = style.Render(tabs[i])
	}
	return lipgloss.NewStyle().Bold(true).Render("easy-llm-router") + "  " + strings.Join(tabs, " ")
}

func (m Model) viewPools() string {
	cfg, state := m.backend.Manager.Snapshot()
	if len(cfg.Pools) == 0 {
		return "No route pools. Press p to create one."
	}
	pool := cfg.Pools[min(m.poolIndex, len(cfg.Pools)-1)]
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/pools/%s", cfg.Settings.Port, pool.Name)
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).Render(pool.Name) + "  " + endpoint, ""}
	now := time.Now()
	budget := contentWidth(m.width)
	credentials := credentialMap(cfg)
	showNextVerify, showRecovery := poolColumnBudget(budget)
	for i, id := range pool.CredentialIDs {
		credential := credentials[id]
		credentialState := state.Credentials[id]
		marker := "  "
		if i == m.cursor {
			marker = "> "
		}
		current := " "
		if state.Current[pool.Name] == id {
			current = "*"
		}
		epoch := credentialState.QuotaEpoch
		if credentialState.Status != domain.StatusExhausted {
			epoch = ""
		}
		nextVerify := ""
		if showNextVerify && credentialState.Status == domain.StatusExhausted && !credentialState.NextVerifyAt.IsZero() {
			nextVerify = formatTime(credentialState.NextVerifyAt, now)
		}
		recovery := ""
		if showRecovery && credentialState.Status == domain.StatusExhausted && !credentialState.RecoveryHint.IsZero() && credentialState.RecoveryHint.After(now) {
			recovery = formatTime(credentialState.RecoveryHint, now)
		}
		layout := "%s%s %-22s %-12s %-14s"
		args := []any{marker, current, credential.Name, credentialState.Status, epoch}
		if showNextVerify {
			layout += " %8s"
			args = append(args, nextVerify)
		}
		if showRecovery {
			layout += " %8s"
			args = append(args, recovery)
		}
		lines = append(lines, fmt.Sprintf(layout, args...))
	}
	return strings.Join(lines, "\n")
}

// poolColumnBudget decides which time columns fit in the budget. Time columns
// drop from right to left: recovery is dropped first, then next-verify. Name,
// status and epoch stay visible at any width.
func poolColumnBudget(width int) (nextVerify, recovery bool) {
	const (
		baseWidth      = 2 + 1 + 22 + 1 + 12 + 1 + 14
		nextVerifyCell = 1 + 8
		recoveryCell   = 1 + 8
	)
	if width >= baseWidth+nextVerifyCell+recoveryCell {
		return true, true
	}
	if width >= baseWidth+nextVerifyCell {
		return true, false
	}
	return false, false
}

func formatTime(target, now time.Time) string {
	remaining := target.Sub(now)
	switch {
	case remaining < -time.Hour:
		return "—"
	case remaining < time.Minute:
		return fmt.Sprintf("in %ds", int(remaining.Seconds()))
	case remaining < time.Hour:
		return fmt.Sprintf("in %dm", int(remaining.Minutes()))
	default:
		return fmt.Sprintf("in %dh", int(remaining.Hours()))
	}
}

func (m Model) viewServices() string {
	cfg, _ := m.backend.Manager.Snapshot()
	if len(cfg.Services) == 0 {
		return "No services. Press a to add one."
	}
	var lines []string
	for i, service := range cfg.Services {
		marker := "  "
		if i == m.cursor {
			marker = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%-24s %-20s %s", marker, service.Name, service.ID, service.BaseURL))
	}
	service := cfg.Services[min(m.cursor, len(cfg.Services)-1)]
	lines = append(lines, "", fmt.Sprintf("Probe: %s / %s", service.Probe.Protocol, service.Probe.Model), fmt.Sprintf("Rules: %d", len(service.Rules)))
	return strings.Join(lines, "\n")
}

func (m Model) viewLogs() string {
	entries := m.backend.Logs.Lines()
	if m.logFilter != "" {
		entries = slices.DeleteFunc(entries, func(line string) bool { return !strings.Contains(strings.ToLower(line), strings.ToLower(m.logFilter)) })
	}
	if len(entries) == 0 {
		return "No matching log entries."
	}
	budget := max(5, m.height-9)
	width := max(20, m.width-6)
	var view []string
	used := 0
	for i := len(entries) - 1; i >= 0 && used < budget; i-- {
		wrapped := strings.Split(lipgloss.Wrap(entries[i], width-2, " "), "\n")
		for j := 1; j < len(wrapped); j++ {
			wrapped[j] = "  " + wrapped[j]
		}
		if take := budget - used; take < len(wrapped) {
			wrapped = wrapped[len(wrapped)-take:]
		}
		view = append(wrapped, view...)
		used += len(wrapped)
	}
	return strings.Join(view, "\n")
}

func (m Model) viewSettings() string {
	cfg, _ := m.backend.Manager.Snapshot()
	return fmt.Sprintf("Listen            127.0.0.1:%d\nLog level         %s\nReplay memory     %d MiB\nReplay limit      %d MiB\nResponse inspect  %d MiB\nShutdown timeout  %s\n\nConfig       %s\nCredentials  %s\nState        %s",
		cfg.Settings.Port, cfg.Settings.LogLevel, cfg.Settings.ReplayMemoryLimit>>20, cfg.Settings.ReplayLimit>>20,
		cfg.Settings.ResponseInspectLimit>>20, cfg.Settings.ShutdownTimeout, m.backend.Paths.Config, m.backend.Paths.Credentials, m.backend.Paths.State)
}

func (m Model) help() string {
	width := contentWidth(m.width)
	switch m.screen {
	case screenPools:
		return renderHelp(width,
			helpBinding{"[[ / ]]", "pool"}, helpBinding{"[j/k]", "select"}, helpBinding{"[a]", "add key"},
			helpBinding{"[e]", "edit"}, helpBinding{"[x]", "delete"}, helpBinding{"[J/K]", "reorder"},
			helpBinding{"[m]", "current"}, helpBinding{"[d]", "disable"}, helpBinding{"[z]", "exhausted"},
			helpBinding{"[v]", "verify"}, helpBinding{"[V]", "verify all"}, helpBinding{"[p]", "add pool"},
			helpBinding{"[n]", "rename/interval"}, helpBinding{"[X]", "delete pool"}, helpBinding{"[q]", "quit"},
		)
	case screenServices:
		return renderHelp(width,
			helpBinding{"[j/k]", "select"}, helpBinding{"[a]", "add"}, helpBinding{"[e]", "edit"},
			helpBinding{"[x]", "delete"}, helpBinding{"[r]", "add rule"}, helpBinding{"[R]", "delete rule"},
			helpBinding{"[t]", "test rules"}, helpBinding{"[q]", "quit"},
		)
	case screenLogs:
		return renderHelp(width,
			helpBinding{"[f]", "filter"}, helpBinding{"[c]", "clear filter"}, helpBinding{"[q]", "quit"},
		)
	case screenSettings:
		return renderHelp(width,
			helpBinding{"[e]", "edit settings"}, helpBinding{"[q]", "quit"},
		)
	default:
		return ""
	}
}

func (m Model) handleAction(key string) (tea.Model, tea.Cmd) {
	if m.confirm != "" && key != "V" && key != "x" && key != "X" {
		m.confirm = ""
	}
	switch m.screen {
	case screenPools:
		return m.poolAction(key)
	case screenServices:
		return m.serviceAction(key)
	case screenLogs:
		if key == "f" {
			m.openForm("filter", []string{"Contains"}, []string{m.logFilter}, -1)
		} else if key == "c" {
			m.logFilter = ""
		}
	case screenSettings:
		if key == "e" {
			cfg, _ := m.backend.Manager.Snapshot()
			m.openForm("settings", []string{"Port", "Log level", "Replay memory MiB", "Replay limit MiB", "Inspect limit MiB", "Shutdown seconds"}, []string{
				strconv.Itoa(cfg.Settings.Port), cfg.Settings.LogLevel, strconv.FormatInt(cfg.Settings.ReplayMemoryLimit>>20, 10),
				strconv.FormatInt(cfg.Settings.ReplayLimit>>20, 10), strconv.FormatInt(cfg.Settings.ResponseInspectLimit>>20, 10), strconv.Itoa(int(cfg.Settings.ShutdownTimeout.Seconds())),
			}, -1)
		}
	}
	return m, nil
}

func (m Model) poolAction(key string) (tea.Model, tea.Cmd) {
	cfg, state := m.backend.Manager.Snapshot()
	if key == "[" && len(cfg.Pools) > 0 {
		m.poolIndex = (m.poolIndex - 1 + len(cfg.Pools)) % len(cfg.Pools)
		m.cursor = 0
		return m, nil
	}
	if key == "]" && len(cfg.Pools) > 0 {
		m.poolIndex = (m.poolIndex + 1) % len(cfg.Pools)
		m.cursor = 0
		return m, nil
	}
	if key == "p" {
		m.openForm("pool", []string{"Pool name", "Credential IDs (comma separated)", "Verify interval"}, []string{"new-pool", "", "5m"}, -1)
		return m, nil
	}
	if len(cfg.Pools) == 0 {
		return m, nil
	}
	pool := cfg.Pools[min(m.poolIndex, len(cfg.Pools)-1)]
	if key == "n" {
		m.openForm("pool-edit", []string{"Pool name", "Verify interval"}, []string{pool.Name, pool.VerifyInterval.String()}, -1)
		m.form.editID = pool.Name
		return m, nil
	}
	if key == "X" {
		if m.confirm != "delete-pool" {
			m.confirm = "delete-pool"
			m.status = "press X again to delete this pool"
			return m, nil
		}
		m.confirm = ""
		cfg.Pools = slices.Delete(cfg.Pools, m.poolIndex, m.poolIndex+1)
		if err := m.backend.Save(cfg, m.backend.Manager.Secrets()); err != nil {
			m.status = err.Error()
		} else {
			m.poolIndex, m.cursor = 0, 0
			m.status = "pool deleted"
		}
		return m, nil
	}
	if key == "a" {
		serviceID := ""
		if len(cfg.Services) > 0 {
			serviceID = cfg.Services[0].ID
		}
		m.openForm("credential", []string{"Name", "Service ID", "API key", "Base URL override"}, []string{"new-key", serviceID, "", ""}, -1)
		return m, nil
	}
	if len(pool.CredentialIDs) == 0 {
		return m, nil
	}
	index := min(m.cursor, len(pool.CredentialIDs)-1)
	id := pool.CredentialIDs[index]
	credential := credentialMap(cfg)[id]
	switch key {
	case "e":
		apiKey := m.backend.Manager.Secrets().APIKeys[id]
		m.openForm("credential", []string{"Name", "Service ID", "API key", "Base URL override"}, []string{credential.Name, credential.ServiceID, apiKey, credential.BaseURLOverride}, index)
		m.form.editID = id
	case "x":
		if m.confirm != "delete-credential" {
			m.confirm = "delete-credential"
			m.status = "press x again to delete this credential"
			return m, nil
		}
		m.confirm = ""
		pool.CredentialIDs = slices.Delete(pool.CredentialIDs, index, index+1)
		cfg.Pools[m.poolIndex] = pool
		if len(pool.CredentialIDs) == 0 {
			m.status = "a pool must retain at least one credential"
			return m, nil
		}
		cfg.Credentials = slices.DeleteFunc(cfg.Credentials, func(item domain.Credential) bool { return item.ID == id })
		secrets := m.backend.Manager.Secrets()
		delete(secrets.APIKeys, id)
		if err := m.backend.Save(cfg, secrets); err != nil {
			m.status = err.Error()
		} else {
			m.cursor = max(0, m.cursor-1)
			m.status = "credential deleted"
		}
	case "J", "K":
		delta := 1
		if key == "K" {
			delta = -1
		}
		other := index + delta
		if other >= 0 && other < len(pool.CredentialIDs) {
			pool.CredentialIDs[index], pool.CredentialIDs[other] = pool.CredentialIDs[other], pool.CredentialIDs[index]
			cfg.Pools[m.poolIndex] = pool
			if err := m.backend.Save(cfg, m.backend.Manager.Secrets()); err != nil {
				m.status = err.Error()
			} else {
				m.cursor = other
			}
		}
	case "m":
		if err := m.backend.Manager.SetCurrent(pool.Name, id); err != nil {
			m.status = err.Error()
		} else {
			m.status = "current credential changed"
		}
	case "d":
		status := state.Credentials[id].Status
		if status == domain.StatusDisabled {
			if err := m.backend.Manager.SetStatus(pool.Name, id, domain.StatusUnknown, "user restored"); err != nil {
				m.status = err.Error()
			} else {
				m.status = "credential restored as unknown; validate it"
			}
		} else if err := m.backend.Manager.SetStatus(pool.Name, id, domain.StatusDisabled, "user disabled"); err != nil {
			m.status = err.Error()
		}
	case "z":
		if err := m.backend.Manager.SetStatus(pool.Name, id, domain.StatusExhausted, "user marked exhausted"); err != nil {
			m.status = err.Error()
		}
	case "v":
		return m, validateCmd(m.backend.Runner, pool.Name, id)
	case "V":
		if m.confirm != "validate-all" {
			m.confirm = "validate-all"
			m.status = "press V again to send a validation request with every non-disabled credential"
			return m, nil
		}
		m.confirm = ""
		return m, validateAllCmd(m.backend.Runner)
	}
	return m, nil
}

func (m Model) serviceAction(key string) (tea.Model, tea.Cmd) {
	cfg, _ := m.backend.Manager.Snapshot()
	if key == "a" {
		m.openForm("service", serviceLabels(), []string{"custom", "Custom", "https://example.com/v1", "Authorization", "Bearer ", string(domain.ProbeCustom), "", "POST", "/chat/completions", "{}", "{}"}, -1)
		return m, nil
	}
	if len(cfg.Services) == 0 {
		return m, nil
	}
	index := min(m.cursor, len(cfg.Services)-1)
	service := cfg.Services[index]
	switch key {
	case "e":
		m.openForm("service", serviceLabels(), serviceValues(service), index)
		m.form.editID = service.ID
	case "x":
		if m.confirm != "delete-service" {
			m.confirm = "delete-service"
			m.status = "press x again to delete this service"
			return m, nil
		}
		m.confirm = ""
		cfg.Services = slices.Delete(cfg.Services, index, index+1)
		if err := m.backend.Save(cfg, m.backend.Manager.Secrets()); err != nil {
			m.status = err.Error()
		} else {
			m.cursor = max(0, m.cursor-1)
			m.status = "service deleted"
		}
	case "r":
		m.openForm("rule", []string{"Name", "Result", "Status min", "Status max", "Header", "Header operator", "Header value", "JSON path", "JSON operator", "JSON value", "Body regex"}, []string{"new-rule", string(domain.ClassExhausted), "429", "429", "", string(domain.MatchEquals), "", "error.type", string(domain.MatchEquals), "QuotaError", ""}, index)
		m.form.editID = service.ID
	case "R":
		m.openForm("delete-rule", []string{"Rule name"}, []string{""}, index)
		m.form.editID = service.ID
	case "t":
		m.openForm("test-rule", []string{"HTTP status", "Response body"}, []string{"429", `{"error":{"type":"QuotaError"}}`}, index)
		m.form.editID = service.ID
	}
	return m, nil
}

func (m *Model) moveCursor(delta int) {
	count := 0
	cfg, _ := m.backend.Manager.Snapshot()
	if m.screen == screenPools && len(cfg.Pools) > 0 {
		count = len(cfg.Pools[min(m.poolIndex, len(cfg.Pools)-1)].CredentialIDs)
	} else if m.screen == screenServices {
		count = len(cfg.Services)
	}
	if count > 0 {
		m.cursor = (m.cursor + delta + count) % count
	}
}

func (m *Model) openForm(kind string, labels, values []string, editIndex int) {
	inputs := make([]textinput.Model, len(labels))
	for i := range labels {
		inputs[i] = textinput.New()
		inputs[i].Prompt = ""
		inputs[i].Placeholder = labels[i]
		inputs[i].SetValue(values[i])
		inputs[i].SetWidth(inputWidth(m.width, 30))
	}
	inputs[0].Focus()
	m.form = &form{kind: kind, labels: labels, inputs: inputs, editID: ""}
	_ = editIndex
}

func (m Model) updateForm(message tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := message.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		for i := range m.form.inputs {
			m.form.inputs[i].SetWidth(inputWidth(size.Width, 30))
		}
		return m, nil
	}
	if key, ok := message.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			m.form = nil
			return m, nil
		case "tab", "down":
			m.form.inputs[m.form.focus].Blur()
			m.form.focus = (m.form.focus + 1) % len(m.form.inputs)
			m.form.inputs[m.form.focus].Focus()
			return m, nil
		case "shift+tab", "up":
			m.form.inputs[m.form.focus].Blur()
			m.form.focus = (m.form.focus - 1 + len(m.form.inputs)) % len(m.form.inputs)
			m.form.inputs[m.form.focus].Focus()
			return m, nil
		case "enter":
			if m.form.focus < len(m.form.inputs)-1 {
				m.form.inputs[m.form.focus].Blur()
				m.form.focus++
				m.form.inputs[m.form.focus].Focus()
				return m, nil
			}
			if err := m.submitForm(); err != nil {
				m.status = err.Error()
				return m, nil
			}
			m.form = nil
			return m, nil
		}
	}
	updated, cmd := m.form.inputs[m.form.focus].Update(message)
	m.form.inputs[m.form.focus] = updated
	return m, cmd
}

func (m Model) viewForm() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2")).Render(strings.ReplaceAll(m.form.kind, "-", " "))
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Width(22)
	maxFields := max(3, (m.height-7)/2)
	if maxFields > len(m.form.inputs) {
		maxFields = len(m.form.inputs)
	}
	start := max(0, m.form.focus-maxFields/2)
	if start+maxFields > len(m.form.inputs) {
		start = max(0, len(m.form.inputs)-maxFields)
	}
	end := min(len(m.form.inputs), start+maxFields)
	var lines []string
	for i := start; i < end; i++ {
		input := m.form.inputs[i]
		marker := "  "
		if i == m.form.focus {
			marker = "> "
		}
		lines = append(lines, marker+label.Render(m.form.labels[i])+input.View())
	}
	position := fmt.Sprintf("fields %d-%d of %d", start+1, end, len(m.form.inputs))
	help := renderHelp(contentWidth(m.width),
		helpBinding{"[Tab]", "move"}, helpBinding{"[Enter]", "save"}, helpBinding{"[Esc]", "cancel"},
	)
	return title + "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(position) + "\n\n" + strings.Join(lines, "\n\n") + "\n\n" + help
}

func (m *Model) submitForm() error {
	values := make([]string, len(m.form.inputs))
	for i := range values {
		values[i] = strings.TrimSpace(m.form.inputs[i].Value())
	}
	cfg, _ := m.backend.Manager.Snapshot()
	secrets := m.backend.Manager.Secrets()
	switch m.form.kind {
	case "filter":
		m.logFilter = values[0]
		return nil
	case "credential":
		id := m.form.editID
		if id == "" {
			id = uniqueID("credential", func(candidate string) bool { return credentialMap(cfg)[candidate].ID != "" })
			cfg.Credentials = append(cfg.Credentials, domain.Credential{ID: id})
			cfg.Pools[m.poolIndex].CredentialIDs = append(cfg.Pools[m.poolIndex].CredentialIDs, id)
		}
		for i := range cfg.Credentials {
			if cfg.Credentials[i].ID == id {
				cfg.Credentials[i].Name, cfg.Credentials[i].ServiceID, cfg.Credentials[i].BaseURLOverride = values[0], values[1], values[3]
			}
		}
		apiKey := m.form.inputs[2].Value()
		if apiKey != "" {
			secrets.APIKeys[id] = apiKey
		}
	case "pool":
		interval, err := time.ParseDuration(values[2])
		if err != nil {
			return err
		}
		ids := splitComma(values[1])
		cfg.Pools = append(cfg.Pools, domain.Pool{Name: values[0], CredentialIDs: ids, VerifyInterval: interval})
	case "pool-edit":
		interval, err := time.ParseDuration(values[1])
		if err != nil {
			return err
		}
		for i := range cfg.Pools {
			if cfg.Pools[i].Name == m.form.editID {
				cfg.Pools[i].Name = values[0]
				cfg.Pools[i].VerifyInterval = interval
			}
		}
	case "service":
		probeHeaders := map[string]string{}
		if values[9] != "" {
			if err := json.Unmarshal([]byte(m.form.inputs[9].Value()), &probeHeaders); err != nil {
				return fmt.Errorf("probe headers: %w", err)
			}
		}
		probeBody := map[string]any{}
		if values[10] != "" {
			if err := json.Unmarshal([]byte(m.form.inputs[10].Value()), &probeBody); err != nil {
				return fmt.Errorf("probe body: %w", err)
			}
		}
		service := domain.Service{
			ID: values[0], Name: values[1], BaseURL: values[2], AuthHeader: values[3], AuthPrefix: m.form.inputs[4].Value(),
			Probe: domain.ProbeConfig{Protocol: domain.ProbeProtocol(values[5]), Model: values[6], Method: values[7], Path: values[8], Headers: probeHeaders, Body: probeBody},
		}
		if m.form.editID == "" {
			service.Rules = []domain.ResponseRule{{Name: "http-authentication-failed", Result: domain.ClassInvalid, Conditions: []domain.Condition{{StatusMin: 401, StatusMax: 401}}}}
			cfg.Services = append(cfg.Services, service)
		} else {
			for i := range cfg.Services {
				if cfg.Services[i].ID == m.form.editID {
					service.Rules, service.Preset = cfg.Services[i].Rules, cfg.Services[i].Preset
					cfg.Services[i] = service
				}
			}
			if service.ID != m.form.editID {
				for i := range cfg.Credentials {
					if cfg.Credentials[i].ServiceID == m.form.editID {
						cfg.Credentials[i].ServiceID = service.ID
					}
				}
			}
		}
	case "rule":
		minStatus, _ := strconv.Atoi(values[2])
		maxStatus, _ := strconv.Atoi(values[3])
		conditions := []domain.Condition{}
		if minStatus > 0 {
			conditions = append(conditions, domain.Condition{StatusMin: minStatus, StatusMax: maxStatus})
		}
		if values[4] != "" {
			conditions = append(conditions, domain.Condition{Header: values[4], Operator: domain.MatchOperator(values[5]), Value: values[6]})
		}
		if values[7] != "" {
			conditions = append(conditions, domain.Condition{JSONPath: values[7], Operator: domain.MatchOperator(values[8]), Value: values[9]})
		}
		if values[10] != "" {
			conditions = append(conditions, domain.Condition{Body: true, Operator: domain.MatchRegex, Value: values[10]})
		}
		for i := range cfg.Services {
			if cfg.Services[i].ID == m.form.editID {
				cfg.Services[i].Rules = append(cfg.Services[i].Rules, domain.ResponseRule{Name: values[0], Result: domain.Classification(values[1]), Conditions: conditions})
			}
		}
	case "delete-rule":
		found := false
		for i := range cfg.Services {
			if cfg.Services[i].ID == m.form.editID {
				before := len(cfg.Services[i].Rules)
				cfg.Services[i].Rules = slices.DeleteFunc(cfg.Services[i].Rules, func(rule domain.ResponseRule) bool { return rule.Name == values[0] })
				found = len(cfg.Services[i].Rules) != before
			}
		}
		if !found {
			return fmt.Errorf("rule %q not found", values[0])
		}
	case "test-rule":
		status, err := strconv.Atoi(values[0])
		if err != nil {
			return err
		}
		for _, service := range cfg.Services {
			if service.ID == m.form.editID {
				match := classify.Evaluate(service.Rules, classify.Response{StatusCode: status, Header: http.Header{}, Body: []byte(m.form.inputs[1].Value())})
				m.status = fmt.Sprintf("classification: %s (%s)", match.Result, match.RuleName)
				return nil
			}
		}
		return fmt.Errorf("service not found")
	case "settings":
		port, err := strconv.Atoi(values[0])
		if err != nil {
			return err
		}
		memory, err := strconv.ParseInt(values[2], 10, 64)
		if err != nil {
			return err
		}
		replay, err := strconv.ParseInt(values[3], 10, 64)
		if err != nil {
			return err
		}
		inspect, err := strconv.ParseInt(values[4], 10, 64)
		if err != nil {
			return err
		}
		shutdown, err := strconv.Atoi(values[5])
		if err != nil {
			return err
		}
		cfg.Settings.Port, cfg.Settings.LogLevel = port, values[1]
		cfg.Settings.ReplayMemoryLimit, cfg.Settings.ReplayLimit, cfg.Settings.ResponseInspectLimit = memory<<20, replay<<20, inspect<<20
		cfg.Settings.ShutdownTimeout = time.Duration(shutdown) * time.Second
	}
	if err := m.backend.Save(cfg, secrets); err != nil {
		return err
	}
	if m.form.kind == "settings" {
		m.status = "saved; listener and log changes apply after restart"
	} else {
		m.status = "saved"
	}
	return nil
}

func (m Model) shutdownCmd() tea.Cmd {
	return func() tea.Msg {
		cfg, _ := m.backend.Manager.Snapshot()
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Settings.ShutdownTimeout)
		defer cancel()
		return shutdownMsg{err: m.backend.Shutdown(ctx)}
	}
}

func validateCmd(runner interface {
	Validate(context.Context, string, string) (domain.Classification, error)
}, pool, id string) tea.Cmd {
	return func() tea.Msg {
		result, err := runner.Validate(context.Background(), pool, id)
		return validationMsg{label: "validate " + id, result: result, err: err}
	}
}

func validateAllCmd(runner interface {
	ValidateAll(context.Context) map[string]error
}) tea.Cmd {
	return func() tea.Msg {
		results := runner.ValidateAll(context.Background())
		failed := 0
		for _, err := range results {
			if err != nil {
				failed++
			}
		}
		return validationMsg{label: fmt.Sprintf("validated %d credential(s), %d failed", len(results), failed), result: domain.ClassSuccess}
	}
}

func tick() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func credentialMap(cfg domain.Config) map[string]domain.Credential {
	result := make(map[string]domain.Credential, len(cfg.Credentials))
	for _, credential := range cfg.Credentials {
		result[credential.ID] = credential
	}
	return result
}

func uniqueID(prefix string, exists func(string) bool) string {
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s-%d", prefix, i)
		if !exists(candidate) {
			return candidate
		}
	}
}

func splitComma(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func serviceLabels() []string {
	return []string{"ID", "Name", "Base URL", "Auth header", "Auth prefix", "Probe protocol", "Probe model", "Probe method", "Probe path", "Probe headers JSON", "Probe body JSON"}
}

func serviceValues(service domain.Service) []string {
	headers, _ := json.Marshal(service.Probe.Headers)
	body, _ := json.Marshal(service.Probe.Body)
	return []string{
		service.ID, service.Name, service.BaseURL, service.AuthHeader, service.AuthPrefix,
		string(service.Probe.Protocol), service.Probe.Model, service.Probe.Method, service.Probe.Path, string(headers), string(body),
	}
}

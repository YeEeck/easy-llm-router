package routing

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/yeck/easy-llm-router/internal/domain"
)

var ErrNoCredential = errors.New("no selectable credential")

type SaveFunc func(domain.RuntimeState) error

type Selection struct {
	Pool       domain.Pool
	Credential domain.Credential
	Service    domain.Service
	APIKey     string
	Status     domain.CredentialStatus
	LastResort bool
}

type Manager struct {
	mu      sync.RWMutex
	config  domain.Config
	secrets domain.Secrets
	state   domain.RuntimeState
	save    SaveFunc
	now     func() time.Time
}

func New(cfg domain.Config, secrets domain.Secrets, state domain.RuntimeState, save SaveFunc) (*Manager, error) {
	manager := &Manager{config: cfg, secrets: secrets, state: state, save: save, now: time.Now}
	manager.normalizeLocked()
	if err := manager.validateReferencesLocked(); err != nil {
		return nil, err
	}
	return manager, nil
}

func (m *Manager) Start(poolName string, attempted map[string]bool) (Selection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pool, ok := m.poolLocked(poolName)
	if !ok {
		return Selection{}, fmt.Errorf("pool %q: %w", poolName, ErrNoCredential)
	}
	current := m.state.Current[poolName]
	if current != "" && !attempted[current] && m.statusLocked(current) == domain.StatusAvailable {
		return m.selectionLocked(pool, current, false)
	}
	if id := m.nextAvailableLocked(pool, current, attempted); id != "" {
		m.state.Current[poolName] = id
		m.persistLocked()
		return m.selectionLocked(pool, id, false)
	}
	if current != "" && !attempted[current] && m.statusLocked(current) == domain.StatusExhausted {
		return m.selectionLocked(pool, current, true)
	}
	return Selection{}, fmt.Errorf("pool %q: %w", poolName, ErrNoCredential)
}

func (m *Manager) Next(poolName, failedID string, attempted map[string]bool) (Selection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pool, ok := m.poolLocked(poolName)
	if !ok {
		return Selection{}, fmt.Errorf("pool %q: %w", poolName, ErrNoCredential)
	}
	id := m.nextAvailableLocked(pool, failedID, attempted)
	if id == "" {
		return Selection{}, fmt.Errorf("pool %q: %w", poolName, ErrNoCredential)
	}
	m.state.Current[poolName] = id
	m.persistLocked()
	return m.selectionLocked(pool, id, false)
}

func (m *Manager) Transition(poolName, credentialID string, result domain.Classification, reason string, recoveryHint time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.state.Credentials[credentialID]
	status := current.Status
	switch result {
	case domain.ClassSuccess:
		status = domain.StatusAvailable
	case domain.ClassExhausted:
		status = domain.StatusExhausted
	case domain.ClassInvalid:
		status = domain.StatusInvalid
	case domain.ClassInconclusive:
		return nil
	default:
		return fmt.Errorf("unknown classification %q", result)
	}
	if status == current.Status && reason == current.Reason && status != domain.StatusExhausted {
		return nil
	}
	current.Status = status
	current.ChangedAt = m.now().UTC()
	current.Reason = reason
	if status == domain.StatusExhausted {
		current.RecoveryHint = recoveryHint
		if pool, ok := m.poolLocked(poolName); ok {
			current.NextVerifyAt = current.ChangedAt.Add(pool.VerifyInterval)
		}
	} else {
		current.NextVerifyAt = time.Time{}
		current.RecoveryHint = time.Time{}
		current.LastValidation = ""
	}
	m.state.Credentials[credentialID] = current
	return m.persistLocked()
}

func (m *Manager) SetStatus(poolName, credentialID string, status domain.CredentialStatus, reason string) error {
	if !status.Valid() {
		return fmt.Errorf("invalid credential status %q", status)
	}
	if status == domain.StatusAvailable {
		return errors.New("available status can only result from a request or validation")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.credentialLocked(credentialID); !ok {
		return fmt.Errorf("unknown credential %q", credentialID)
	}
	state := m.state.Credentials[credentialID]
	state.Status = status
	state.ChangedAt = m.now().UTC()
	state.Reason = reason
	state.NextVerifyAt = time.Time{}
	state.RecoveryHint = time.Time{}
	state.LastValidation = ""
	if status == domain.StatusExhausted {
		if pool, ok := m.poolLocked(poolName); ok {
			state.NextVerifyAt = state.ChangedAt.Add(pool.VerifyInterval)
		}
	}
	m.state.Credentials[credentialID] = state
	return m.persistLocked()
}

func (m *Manager) ScheduleNextVerification(poolName, credentialID, note string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.state.Credentials[credentialID]
	if state.Status != domain.StatusExhausted {
		return nil
	}
	pool, ok := m.poolLocked(poolName)
	if !ok {
		return fmt.Errorf("unknown pool %q", poolName)
	}
	state.NextVerifyAt = m.now().UTC().Add(pool.VerifyInterval)
	state.LastValidation = note
	m.state.Credentials[credentialID] = state
	return m.persistLocked()
}

func (m *Manager) SetCurrent(poolName, credentialID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	pool, ok := m.poolLocked(poolName)
	if !ok || !slices.Contains(pool.CredentialIDs, credentialID) {
		return fmt.Errorf("credential %q is not in pool %q", credentialID, poolName)
	}
	if m.statusLocked(credentialID) != domain.StatusAvailable {
		return fmt.Errorf("credential %q is not available", credentialID)
	}
	m.state.Current[poolName] = credentialID
	return m.persistLocked()
}

func (m *Manager) Snapshot() (domain.Config, domain.RuntimeState) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneConfig(m.config), cloneState(m.state)
}

func (m *Manager) Secrets() domain.Secrets {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make(map[string]string, len(m.secrets.APIKeys))
	for id, key := range m.secrets.APIKeys {
		keys[id] = key
	}
	return domain.Secrets{Version: m.secrets.Version, APIKeys: keys}
}

func (m *Manager) ReplaceConfig(cfg domain.Config, secrets domain.Secrets) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	oldConfig, oldSecrets := m.config, m.secrets
	m.config, m.secrets = cfg, secrets
	m.normalizeLocked()
	if err := m.validateReferencesLocked(); err != nil {
		m.config, m.secrets = oldConfig, oldSecrets
		return err
	}
	return m.persistLocked()
}

func (m *Manager) DueForVerification(now time.Time) []Selection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var due []Selection
	for _, pool := range m.config.Pools {
		for _, id := range pool.CredentialIDs {
			state := m.state.Credentials[id]
			if state.Status != domain.StatusExhausted || state.NextVerifyAt.After(now) {
				continue
			}
			selection, err := m.selectionLocked(pool, id, false)
			if err == nil {
				due = append(due, selection)
			}
		}
	}
	return due
}

func (m *Manager) UnknownSelections() []Selection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	seen := map[string]bool{}
	var unknown []Selection
	for _, pool := range m.config.Pools {
		for _, id := range pool.CredentialIDs {
			if seen[id] || m.statusLocked(id) != domain.StatusUnknown {
				continue
			}
			selection, err := m.selectionLocked(pool, id, false)
			if err == nil {
				unknown = append(unknown, selection)
				seen[id] = true
			}
		}
	}
	return unknown
}

func (m *Manager) Selection(poolName, credentialID string) (Selection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	pool, ok := m.poolLocked(poolName)
	if !ok || !slices.Contains(pool.CredentialIDs, credentialID) {
		return Selection{}, ErrNoCredential
	}
	return m.selectionLocked(pool, credentialID, false)
}

func (m *Manager) poolLocked(name string) (domain.Pool, bool) {
	for _, pool := range m.config.Pools {
		if pool.Name == name {
			return pool, true
		}
	}
	return domain.Pool{}, false
}

func (m *Manager) credentialLocked(id string) (domain.Credential, bool) {
	for _, credential := range m.config.Credentials {
		if credential.ID == id {
			return credential, true
		}
	}
	return domain.Credential{}, false
}

func (m *Manager) serviceLocked(id string) (domain.Service, bool) {
	for _, service := range m.config.Services {
		if service.ID == id {
			return service, true
		}
	}
	return domain.Service{}, false
}

func (m *Manager) selectionLocked(pool domain.Pool, id string, lastResort bool) (Selection, error) {
	credential, ok := m.credentialLocked(id)
	if !ok {
		return Selection{}, ErrNoCredential
	}
	service, ok := m.serviceLocked(credential.ServiceID)
	if !ok {
		return Selection{}, ErrNoCredential
	}
	return Selection{
		Pool: pool, Credential: credential, Service: service, APIKey: m.secrets.APIKeys[id],
		Status: m.statusLocked(id), LastResort: lastResort,
	}, nil
}

func (m *Manager) nextAvailableLocked(pool domain.Pool, after string, attempted map[string]bool) string {
	start := 0
	if index := slices.Index(pool.CredentialIDs, after); index >= 0 {
		start = (index + 1) % len(pool.CredentialIDs)
	}
	for offset := range len(pool.CredentialIDs) {
		id := pool.CredentialIDs[(start+offset)%len(pool.CredentialIDs)]
		if !attempted[id] && m.statusLocked(id) == domain.StatusAvailable {
			return id
		}
	}
	return ""
}

func (m *Manager) statusLocked(id string) domain.CredentialStatus {
	status := m.state.Credentials[id].Status
	if !status.Valid() {
		return domain.StatusUnknown
	}
	return status
}

func (m *Manager) normalizeLocked() {
	if m.state.Version == 0 {
		m.state.Version = 1
	}
	if m.state.Credentials == nil {
		m.state.Credentials = map[string]domain.CredentialState{}
	}
	if m.state.Current == nil {
		m.state.Current = map[string]string{}
	}
	for _, credential := range m.config.Credentials {
		state, ok := m.state.Credentials[credential.ID]
		if !ok || !state.Status.Valid() {
			m.state.Credentials[credential.ID] = domain.CredentialState{Status: domain.StatusUnknown}
		}
	}
	for _, pool := range m.config.Pools {
		if !slices.Contains(pool.CredentialIDs, m.state.Current[pool.Name]) && len(pool.CredentialIDs) > 0 {
			m.state.Current[pool.Name] = pool.CredentialIDs[0]
		}
	}
}

func (m *Manager) validateReferencesLocked() error {
	for _, credential := range m.config.Credentials {
		if _, ok := m.serviceLocked(credential.ServiceID); !ok {
			return fmt.Errorf("credential %q references missing service", credential.ID)
		}
		if m.secrets.APIKeys[credential.ID] == "" {
			return fmt.Errorf("credential %q has no API key", credential.ID)
		}
	}
	return nil
}

func (m *Manager) persistLocked() error {
	if m.save == nil {
		return nil
	}
	return m.save(cloneState(m.state))
}

func cloneState(state domain.RuntimeState) domain.RuntimeState {
	result := domain.RuntimeState{Version: state.Version, Credentials: map[string]domain.CredentialState{}, Current: map[string]string{}}
	for id, item := range state.Credentials {
		result.Credentials[id] = item
	}
	for pool, id := range state.Current {
		result.Current[pool] = id
	}
	return result
}

func cloneConfig(cfg domain.Config) domain.Config {
	result := cfg
	result.Services = slices.Clone(cfg.Services)
	result.Credentials = slices.Clone(cfg.Credentials)
	result.Pools = slices.Clone(cfg.Pools)
	for i := range result.Pools {
		result.Pools[i].CredentialIDs = slices.Clone(result.Pools[i].CredentialIDs)
	}
	return result
}

package probe

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yeck/easy-llm-router/internal/classify"
	"github.com/yeck/easy-llm-router/internal/domain"
	"github.com/yeck/easy-llm-router/internal/routing"
)

type Runner struct {
	manager      *routing.Manager
	client       *http.Client
	logger       *slog.Logger
	inspectLimit int64
	mu           sync.Mutex
	running      map[string]bool
}

func NewRunner(manager *routing.Manager, client *http.Client, logger *slog.Logger, inspectLimit int64) *Runner {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{manager: manager, client: client, logger: logger, inspectLimit: inspectLimit, running: map[string]bool{}}
}

func (r *Runner) Validate(ctx context.Context, poolName, credentialID string) (domain.Classification, error) {
	key := poolName + ":" + credentialID
	r.mu.Lock()
	if r.running[key] {
		r.mu.Unlock()
		return domain.ClassInconclusive, fmt.Errorf("validation already running for %s", credentialID)
	}
	r.running[key] = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.running, key)
		r.mu.Unlock()
	}()

	selection, err := r.manager.Selection(poolName, credentialID)
	if err != nil {
		return domain.ClassInconclusive, err
	}
	requestConfig, err := Build(selection.Service.Probe)
	if err != nil {
		return domain.ClassInconclusive, err
	}
	baseURL := selection.Service.BaseURL
	if selection.Credential.BaseURLOverride != "" {
		baseURL = selection.Credential.BaseURLOverride
	}
	request, err := requestConfig.HTTP(baseURL)
	if err != nil {
		return domain.ClassInconclusive, err
	}
	request = request.WithContext(ctx)
	authHeader := selection.Service.AuthHeader
	if authHeader == "" {
		authHeader = "Authorization"
	}
	request.Header.Set(authHeader, selection.Service.AuthPrefix+selection.APIKey)
	response, err := r.client.Do(request)
	if err != nil {
		r.reschedule(poolName, credentialID, "validation transport error")
		return domain.ClassInconclusive, err
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, r.inspectLimit+1))
	if readErr != nil {
		r.reschedule(poolName, credentialID, "validation response read error")
		return domain.ClassInconclusive, readErr
	}
	if int64(len(body)) > r.inspectLimit {
		body = nil
	}
	match := classify.Evaluate(selection.Service.Rules, classify.Response{StatusCode: response.StatusCode, Header: response.Header, Body: body})
	if match.Result == domain.ClassInconclusive {
		r.reschedule(poolName, credentialID, "inconclusive: "+match.RuleName)
	} else {
		recoveryHint, _ := classify.ExtractRecovery(selection.Service.RecoveryHint, classify.Response{StatusCode: response.StatusCode, Header: response.Header, Body: body}, time.Now())
		if err := r.manager.Transition(poolName, credentialID, match.Result, match.RuleName, recoveryHint); err != nil {
			return match.Result, err
		}
	}
	r.logger.Info("credential validation complete", "pool", poolName, "credential", credentialID,
		"status", response.StatusCode, "classification", match.Result, "rule", match.RuleName)
	return match.Result, nil
}

func (r *Runner) ValidateAll(ctx context.Context) map[string]error {
	cfg, state := r.manager.Snapshot()
	results := map[string]error{}
	seen := map[string]bool{}
	for _, pool := range cfg.Pools {
		for _, id := range pool.CredentialIDs {
			if seen[id] || state.Credentials[id].Status == domain.StatusDisabled {
				continue
			}
			seen[id] = true
			_, err := r.Validate(ctx, pool.Name, id)
			results[id] = err
		}
	}
	return results
}

func (r *Runner) ValidateUnknown(ctx context.Context) {
	for _, selection := range r.manager.UnknownSelections() {
		if ctx.Err() != nil {
			return
		}
		_, _ = r.Validate(ctx, selection.Pool.Name, selection.Credential.ID)
	}
}

func (r *Runner) RunScheduler(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			for _, selection := range r.manager.DueForVerification(now) {
				selection := selection
				go func() { _, _ = r.Validate(ctx, selection.Pool.Name, selection.Credential.ID) }()
			}
		}
	}
}

func (r *Runner) reschedule(poolName, credentialID, reason string) {
	if err := r.manager.ScheduleNextVerification(poolName, credentialID, reason); err != nil && !strings.Contains(err.Error(), "unknown") {
		r.logger.Error("schedule next credential validation", "pool", poolName, "credential", credentialID, "error", err)
	}
}

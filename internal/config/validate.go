package config

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/yeck/easy-llm-router/internal/classify"
	"github.com/yeck/easy-llm-router/internal/domain"
	"github.com/yeck/easy-llm-router/internal/probe"
)

var poolNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

func Validate(cfg domain.Config, secrets domain.Secrets) error {
	var problems []error
	if cfg.Version != 1 {
		problems = append(problems, fmt.Errorf("unsupported config version %d", cfg.Version))
	}
	if cfg.Settings.Port < 1 || cfg.Settings.Port > 65535 {
		problems = append(problems, fmt.Errorf("port must be between 1 and 65535"))
	}
	if cfg.Settings.ReplayMemoryLimit < 0 || cfg.Settings.ReplayLimit < cfg.Settings.ReplayMemoryLimit {
		problems = append(problems, fmt.Errorf("replay limit must be at least the memory limit"))
	}
	services := make(map[string]domain.Service, len(cfg.Services))
	for _, service := range cfg.Services {
		if service.ID == "" || services[service.ID].ID != "" {
			problems = append(problems, fmt.Errorf("service IDs must be non-empty and unique: %q", service.ID))
			continue
		}
		services[service.ID] = service
		if parsed, err := url.ParseRequestURI(service.BaseURL); err != nil || parsed.Scheme == "" || parsed.Host == "" {
			problems = append(problems, fmt.Errorf("service %q has invalid base URL", service.ID))
		}
		if err := classify.ValidateRules(service.Rules); err != nil {
			problems = append(problems, fmt.Errorf("service %q rules: %w", service.ID, err))
		}
		if _, err := probe.Build(service.Probe); err != nil {
			problems = append(problems, fmt.Errorf("service %q probe: %w", service.ID, err))
		}
	}
	credentials := make(map[string]domain.Credential, len(cfg.Credentials))
	for _, credential := range cfg.Credentials {
		if credential.ID == "" || credentials[credential.ID].ID != "" {
			problems = append(problems, fmt.Errorf("credential IDs must be non-empty and unique: %q", credential.ID))
			continue
		}
		credentials[credential.ID] = credential
		if _, ok := services[credential.ServiceID]; !ok {
			problems = append(problems, fmt.Errorf("credential %q references unknown service %q", credential.ID, credential.ServiceID))
		}
		if strings.TrimSpace(secrets.APIKeys[credential.ID]) == "" {
			problems = append(problems, fmt.Errorf("credential %q has no API key", credential.ID))
		}
		if credential.BaseURLOverride != "" {
			if parsed, err := url.ParseRequestURI(credential.BaseURLOverride); err != nil || parsed.Scheme == "" || parsed.Host == "" {
				problems = append(problems, fmt.Errorf("credential %q has invalid base URL override", credential.ID))
			}
		}
	}
	pools := make(map[string]struct{}, len(cfg.Pools))
	for _, pool := range cfg.Pools {
		if !poolNamePattern.MatchString(pool.Name) {
			problems = append(problems, fmt.Errorf("pool name %q is invalid", pool.Name))
		}
		if _, ok := pools[pool.Name]; ok {
			problems = append(problems, fmt.Errorf("pool name %q is duplicated", pool.Name))
		}
		pools[pool.Name] = struct{}{}
		if len(pool.CredentialIDs) == 0 {
			problems = append(problems, fmt.Errorf("pool %q has no credentials", pool.Name))
		}
		seen := map[string]struct{}{}
		for _, id := range pool.CredentialIDs {
			if _, ok := credentials[id]; !ok {
				problems = append(problems, fmt.Errorf("pool %q references unknown credential %q", pool.Name, id))
			}
			if _, ok := seen[id]; ok {
				problems = append(problems, fmt.Errorf("pool %q repeats credential %q", pool.Name, id))
			}
			seen[id] = struct{}{}
		}
	}
	return errors.Join(problems...)
}

package tui

import (
	"context"
	"fmt"

	"github.com/yeck/easy-llm-router/internal/config"
	"github.com/yeck/easy-llm-router/internal/domain"
	appLog "github.com/yeck/easy-llm-router/internal/logging"
	"github.com/yeck/easy-llm-router/internal/probe"
	"github.com/yeck/easy-llm-router/internal/routing"
	"github.com/yeck/easy-llm-router/internal/store"
)

type Backend struct {
	Manager  *routing.Manager
	Runner   *probe.Runner
	Logs     *appLog.Hub
	Paths    store.Paths
	Active   func() int64
	Shutdown func(context.Context) error
	Force    func() error
}

func (b Backend) Save(cfg domain.Config, secrets domain.Secrets) error {
	config.ApplyDefaults(&cfg)
	if err := config.Validate(cfg, secrets); err != nil {
		return err
	}
	if err := store.SaveConfig(b.Paths.Config, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	if err := store.SaveSecrets(b.Paths.Credentials, secrets); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}
	return b.Manager.ReplaceConfig(cfg, secrets)
}

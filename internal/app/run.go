package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/yeck/easy-llm-router/internal/config"
	"github.com/yeck/easy-llm-router/internal/domain"
	appLog "github.com/yeck/easy-llm-router/internal/logging"
	"github.com/yeck/easy-llm-router/internal/probe"
	"github.com/yeck/easy-llm-router/internal/router"
	"github.com/yeck/easy-llm-router/internal/routing"
	"github.com/yeck/easy-llm-router/internal/store"
	appTUI "github.com/yeck/easy-llm-router/internal/tui"
)

func Run(ctx context.Context) error {
	paths, err := store.DefaultPaths()
	if err != nil {
		return err
	}
	cfg, secrets, err := loadOrSetup(paths)
	if err != nil {
		return err
	}
	config.ApplyDefaults(&cfg)
	if err := config.Validate(cfg, secrets); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	state, err := store.LoadState(paths.State)
	if store.IsNotExist(err) {
		state = store.NewState()
	} else if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	manager, err := routing.New(cfg, secrets, state, func(state domain.RuntimeState) error { return store.SaveState(paths.State, state) })
	if err != nil {
		return err
	}
	logger, hub, err := appLog.New(paths.Log, cfg.Settings.LogLevel)
	if err != nil {
		return err
	}
	slog.SetDefault(logger)
	if err := router.CleanupTemp(paths.Temp); err != nil {
		logger.Warn("clean stale request bodies", "error", err)
	}
	handler := router.New(router.Options{
		Manager: manager, Logger: logger, ReplayMemoryLimit: cfg.Settings.ReplayMemoryLimit,
		ReplayLimit: cfg.Settings.ReplayLimit, ResponseInspectLimit: cfg.Settings.ResponseInspectLimit, TempDir: paths.Temp,
	})
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", cfg.Settings.Port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 30 * time.Second}
	serveDone := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveDone <- err
	}()
	runner := probe.NewRunner(manager, nil, logger, cfg.Settings.ResponseInspectLimit)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go runner.RunScheduler(runCtx)
	go runner.ValidateUnknown(runCtx)
	backend := appTUI.Backend{
		Manager: manager, Runner: runner, Logs: hub, Paths: paths, Active: handler.ActiveRequests,
		Shutdown: server.Shutdown, Force: server.Close,
	}
	logger.Info("router started", "address", listener.Addr().String(), "pools", len(cfg.Pools))
	_, runErr := tea.NewProgram(appTUI.NewModel(backend)).Run()
	cancel()
	_ = server.Close()
	serveErr := <-serveDone
	return errors.Join(runErr, serveErr)
}

func loadOrSetup(paths store.Paths) (domain.Config, domain.Secrets, error) {
	cfg, configErr := store.LoadConfig(paths.Config)
	secrets, secretErr := store.LoadSecrets(paths.Credentials)
	if configErr == nil && secretErr == nil {
		return cfg, secrets, nil
	}
	if !store.IsNotExist(configErr) && configErr != nil {
		return cfg, secrets, configErr
	}
	if !store.IsNotExist(secretErr) && secretErr != nil {
		return cfg, secrets, secretErr
	}
	result, err := tea.NewProgram(appTUI.NewWizard()).Run()
	if err != nil {
		return cfg, secrets, err
	}
	wizard, ok := result.(appTUI.Wizard)
	if !ok {
		return cfg, secrets, errors.New("setup did not return a result")
	}
	cfg, secrets, ok = wizard.Result()
	if !ok {
		return cfg, secrets, errors.New("setup cancelled")
	}
	if err := config.Validate(cfg, secrets); err != nil {
		return cfg, secrets, err
	}
	if err := store.SaveConfig(paths.Config, cfg); err != nil {
		return cfg, secrets, err
	}
	if err := store.SaveSecrets(paths.Credentials, secrets); err != nil {
		return cfg, secrets, err
	}
	return cfg, secrets, nil
}

func Main() {
	if err := Run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "easy-llm-router:", err)
		os.Exit(1)
	}
}

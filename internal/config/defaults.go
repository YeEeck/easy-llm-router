package config

import (
	"time"

	"github.com/yeck/easy-llm-router/internal/domain"
)

const (
	DefaultPort                 = 8787
	DefaultReplayMemoryLimit    = int64(8 << 20)
	DefaultReplayLimit          = int64(256 << 20)
	DefaultResponseInspectLimit = int64(1 << 20)
)

func Default() domain.Config {
	return domain.Config{
		Version: 1,
		Settings: domain.Settings{
			Port:                 DefaultPort,
			LogLevel:             "info",
			ReplayMemoryLimit:    DefaultReplayMemoryLimit,
			ReplayLimit:          DefaultReplayLimit,
			ResponseInspectLimit: DefaultResponseInspectLimit,
			ShutdownTimeout:      time.Minute,
		},
	}
}

func ApplyDefaults(cfg *domain.Config) {
	defaults := Default()
	if cfg.Version == 0 {
		cfg.Version = defaults.Version
	}
	if cfg.Settings.Port == 0 {
		cfg.Settings.Port = defaults.Settings.Port
	}
	if cfg.Settings.LogLevel == "" {
		cfg.Settings.LogLevel = defaults.Settings.LogLevel
	}
	if cfg.Settings.ReplayMemoryLimit == 0 {
		cfg.Settings.ReplayMemoryLimit = defaults.Settings.ReplayMemoryLimit
	}
	if cfg.Settings.ReplayLimit == 0 {
		cfg.Settings.ReplayLimit = defaults.Settings.ReplayLimit
	}
	if cfg.Settings.ResponseInspectLimit == 0 {
		cfg.Settings.ResponseInspectLimit = defaults.Settings.ResponseInspectLimit
	}
	if cfg.Settings.ShutdownTimeout == 0 {
		cfg.Settings.ShutdownTimeout = defaults.Settings.ShutdownTimeout
	}
	for i := range cfg.Pools {
		if cfg.Pools[i].VerifyInterval == 0 {
			cfg.Pools[i].VerifyInterval = 5 * time.Minute
		}
	}
}

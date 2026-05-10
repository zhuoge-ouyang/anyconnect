package config

import (
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	OriginalGateway    string    `yaml:"original_gateway"`
	SplitTunnelEnabled bool      `yaml:"split_tunnel_enabled"`
	AutoStart          bool      `yaml:"auto_start"`
	UpdateIntervalDays int       `yaml:"update_interval_days"`
	LastUpdate         time.Time `yaml:"last_update"`
	LogLevel           string    `yaml:"log_level"`
}

func DefaultConfig() *Config {
	return &Config{
		SplitTunnelEnabled: true,
		AutoStart:          false,
		UpdateIntervalDays: 7,
		LogLevel:           "info",
	}
}

func configPath() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "configs", "config.yaml")
}

func Load() (*Config, error) {
	path := configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := DefaultConfig()
			_ = cfg.Save()
			return cfg, nil
		}
		return nil, err
	}
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Save() error {
	path := configPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

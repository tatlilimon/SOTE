package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DBPath         string `yaml:"db_path"`
	ListenPort     int    `yaml:"listen_port"`
	TorSocksPort   int    `yaml:"tor_socks_port"`
	TorControlPort int    `yaml:"tor_control_port"`
	LogLevel       string `yaml:"log_level"`
	LogFormat      string `yaml:"log_format"`

	// UseBridges makes Tor connect through the bridges below (for networks
	// that block Tor). See bridges.go for the line syntax.
	UseBridges             bool     `yaml:"use_bridges"`
	Bridges                []string `yaml:"bridges,omitempty"`
	ClientTransportPlugins []string `yaml:"client_transport_plugins,omitempty"`
}

var validLogLevels = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

var validLogFormats = map[string]bool{
	"text": true,
	"json": true,
}

func defaultDBPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".sote", "data.db")
}

func DefaultConfig() *Config {
	return &Config{
		DBPath:         defaultDBPath(),
		ListenPort:     18080,
		TorSocksPort:   9060,
		TorControlPort: 9061,
		LogLevel:       "info",
		LogFormat:      "text",
	}
}

func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	}

	applyEnvOverrides(cfg)

	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func SaveDefaultConfig(path string) error {
	return SaveConfig(path, DefaultConfig())
}

func applyEnvOverrides(cfg *Config) {
	if v, ok := os.LookupEnv("SOTE_DB_PATH"); ok {
		cfg.DBPath = v
	}
	if v, ok := os.LookupEnv("SOTE_LISTEN_PORT"); ok {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.ListenPort = p
		}
	}
	if v, ok := os.LookupEnv("SOTE_TOR_SOCKS_PORT"); ok {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.TorSocksPort = p
		}
	}
	if v, ok := os.LookupEnv("SOTE_TOR_CONTROL_PORT"); ok {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.TorControlPort = p
		}
	}
	if v, ok := os.LookupEnv("SOTE_LOG_LEVEL"); ok {
		cfg.LogLevel = v
	}
	if v, ok := os.LookupEnv("SOTE_LOG_FORMAT"); ok {
		cfg.LogFormat = v
	}
	if v, ok := os.LookupEnv("SOTE_USE_BRIDGES"); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.UseBridges = b
		}
	}
}

func validateConfig(cfg *Config) error {
	for name, port := range map[string]int{
		"listen_port":      cfg.ListenPort,
		"tor_socks_port":   cfg.TorSocksPort,
		"tor_control_port": cfg.TorControlPort,
	} {
		if port < 1024 || port > 65535 {
			return fmt.Errorf("invalid %s: %d (must be 1024-65535)", name, port)
		}
	}

	if !validLogLevels[cfg.LogLevel] {
		return fmt.Errorf("invalid log_level: %q (must be debug, info, warn, or error)", cfg.LogLevel)
	}

	if !validLogFormats[cfg.LogFormat] {
		return fmt.Errorf("invalid log_format: %q (must be text or json)", cfg.LogFormat)
	}

	if err := ValidateBridges(cfg.UseBridges, cfg.Bridges, cfg.ClientTransportPlugins); err != nil {
		return err
	}

	return nil
}

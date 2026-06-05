package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper to create a temp config dir and return its path
func tempConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

// helper to unset all SOTE_ env vars (call in cleanup)
func unsetSoteEnv() {
	for _, k := range []string{
		"SOTE_DB_PATH",
		"SOTE_LISTEN_PORT",
		"SOTE_TOR_SOCKS_PORT",
		"SOTE_TOR_CONTROL_PORT",
		"SOTE_LOG_LEVEL",
		"SOTE_LOG_FORMAT",
	} {
		os.Unsetenv(k)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	unsetSoteEnv()
	dir := tempConfigDir(t)
	cfgPath := filepath.Join(dir, "nonexistent.yaml")

	cfg, err := LoadConfig(cfgPath)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	home, _ := os.UserHomeDir()
	assert.Equal(t, filepath.Join(home, ".sote", "data.db"), cfg.DBPath)
	assert.Equal(t, 18080, cfg.ListenPort)
	assert.Equal(t, 9060, cfg.TorSocksPort)
	assert.Equal(t, 9061, cfg.TorControlPort)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "text", cfg.LogFormat)
}

func TestLoadConfigFromFile(t *testing.T) {
	unsetSoteEnv()
	dir := tempConfigDir(t)
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
db_path: /custom/path/db.sqlite
listen_port: 9090
tor_socks_port: 9050
tor_control_port: 9051
log_level: debug
log_format: json
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yaml), 0644))

	cfg, err := LoadConfig(cfgPath)
	require.NoError(t, err)

	assert.Equal(t, "/custom/path/db.sqlite", cfg.DBPath)
	assert.Equal(t, 9090, cfg.ListenPort)
	assert.Equal(t, 9050, cfg.TorSocksPort)
	assert.Equal(t, 9051, cfg.TorControlPort)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, "json", cfg.LogFormat)
}

func TestEnvVarOverrides(t *testing.T) {
	defer unsetSoteEnv()

	os.Setenv("SOTE_DB_PATH", "/env/db.sqlite")
	os.Setenv("SOTE_LISTEN_PORT", "7777")
	os.Setenv("SOTE_TOR_SOCKS_PORT", "9050")
	os.Setenv("SOTE_TOR_CONTROL_PORT", "9051")
	os.Setenv("SOTE_LOG_LEVEL", "warn")
	os.Setenv("SOTE_LOG_FORMAT", "json")

	dir := tempConfigDir(t)
	cfgPath := filepath.Join(dir, "nonexistent.yaml")

	cfg, err := LoadConfig(cfgPath)
	require.NoError(t, err)

	assert.Equal(t, "/env/db.sqlite", cfg.DBPath)
	assert.Equal(t, 7777, cfg.ListenPort)
	assert.Equal(t, 9050, cfg.TorSocksPort)
	assert.Equal(t, 9051, cfg.TorControlPort)
	assert.Equal(t, "warn", cfg.LogLevel)
	assert.Equal(t, "json", cfg.LogFormat)
}

func TestEnvVarOverridesFileValues(t *testing.T) {
	defer unsetSoteEnv()

	dir := tempConfigDir(t)
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
db_path: /file/db.sqlite
listen_port: 8080
log_level: info
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yaml), 0644))

	// override two of the file values via env
	os.Setenv("SOTE_DB_PATH", "/env/db.sqlite")
	os.Setenv("SOTE_LISTEN_PORT", "9999")

	cfg, err := LoadConfig(cfgPath)
	require.NoError(t, err)

	assert.Equal(t, "/env/db.sqlite", cfg.DBPath, "env should override file")
	assert.Equal(t, 9999, cfg.ListenPort, "env should override file")
	// not set in env, should come from file
	assert.Equal(t, "info", cfg.LogLevel)
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid config",
			cfg:     &Config{ListenPort: 18080, TorSocksPort: 9060, TorControlPort: 9061, LogLevel: "info", LogFormat: "text"},
			wantErr: false,
		},
		{
			name:    "port below 1024",
			cfg:     &Config{ListenPort: 80, TorSocksPort: 9060, TorControlPort: 9061, LogLevel: "info", LogFormat: "text"},
			wantErr: true,
			errMsg:  "listen_port",
		},
		{
			name:    "port above 65535",
			cfg:     &Config{ListenPort: 70000, TorSocksPort: 9060, TorControlPort: 9061, LogLevel: "info", LogFormat: "text"},
			wantErr: true,
			errMsg:  "listen_port",
		},
		{
			name:    "invalid socks port",
			cfg:     &Config{ListenPort: 18080, TorSocksPort: 0, TorControlPort: 9061, LogLevel: "info", LogFormat: "text"},
			wantErr: true,
			errMsg:  "tor_socks_port",
		},
		{
			name:    "invalid log level",
			cfg:     &Config{ListenPort: 18080, TorSocksPort: 9060, TorControlPort: 9061, LogLevel: "verbose", LogFormat: "text"},
			wantErr: true,
			errMsg:  "log_level",
		},
		{
			name:    "invalid log format",
			cfg:     &Config{ListenPort: 18080, TorSocksPort: 9060, TorControlPort: 9061, LogLevel: "info", LogFormat: "xml"},
			wantErr: true,
			errMsg:  "log_format",
		},
		{
			name:    "edge port 1024",
			cfg:     &Config{ListenPort: 1024, TorSocksPort: 9060, TorControlPort: 9061, LogLevel: "info", LogFormat: "text"},
			wantErr: false,
		},
		{
			name:    "edge port 65535",
			cfg:     &Config{ListenPort: 65535, TorSocksPort: 9060, TorControlPort: 9061, LogLevel: "info", LogFormat: "text"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.cfg)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSaveDefaultConfig(t *testing.T) {
	unsetSoteEnv()
	dir := tempConfigDir(t)
	cfgPath := filepath.Join(dir, "config.yaml")

	err := SaveDefaultConfig(cfgPath)
	require.NoError(t, err)

	// verify file exists
	_, err = os.Stat(cfgPath)
	require.NoError(t, err)

	// read back and verify defaults
	cfg, err := LoadConfig(cfgPath)
	require.NoError(t, err)

	home, _ := os.UserHomeDir()
	assert.Equal(t, filepath.Join(home, ".sote", "data.db"), cfg.DBPath)
	assert.Equal(t, 18080, cfg.ListenPort)
	assert.Equal(t, 9060, cfg.TorSocksPort)
	assert.Equal(t, 9061, cfg.TorControlPort)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "text", cfg.LogFormat)
}

func TestSaveDefaultConfigCreatesDir(t *testing.T) {
	unsetSoteEnv()
	dir := tempConfigDir(t)
	// config.yaml is inside a subdirectory that doesn't exist yet
	cfgPath := filepath.Join(dir, "subdir", "nested", "config.yaml")

	err := SaveDefaultConfig(cfgPath)
	require.NoError(t, err)

	_, err = os.Stat(cfgPath)
	require.NoError(t, err)
}

func TestPartialConfigFileKeepsDefaults(t *testing.T) {
	unsetSoteEnv()
	dir := tempConfigDir(t)
	cfgPath := filepath.Join(dir, "partial.yaml")

	// only set listen_port, everything else should stay default
	yaml := `listen_port: 12345`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yaml), 0644))

	cfg, err := LoadConfig(cfgPath)
	require.NoError(t, err)

	assert.Equal(t, 12345, cfg.ListenPort)
	// defaults should remain
	home, _ := os.UserHomeDir()
	assert.Equal(t, filepath.Join(home, ".sote", "data.db"), cfg.DBPath)
	assert.Equal(t, 9060, cfg.TorSocksPort)
	assert.Equal(t, "info", cfg.LogLevel)
}

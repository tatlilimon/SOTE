package logging

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// saveAndRestoreLogger saves the current package-level logger and default logger
// and restores them after the test via t.Cleanup.
func saveAndRestoreLogger(t *testing.T) {
	t.Helper()
	oldLogger := logger
	oldDefault := slog.Default()
	t.Cleanup(func() {
		logger = oldLogger
		slog.SetDefault(oldDefault)
	})
}

func readLogFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err, "failed to read log file")
	return string(data)
}

// ── Level Tests ──────────────────────────────────────────────────────────────

func TestInitLoggerLevels_Debug(t *testing.T) {
	saveAndRestoreLogger(t)
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	InitLogger("debug", "text", logPath)
	Debug("debug message")
	Info("info message")
	Warn("warn message")
	Error("error message")

	content := readLogFile(t, logPath)
	assert.Contains(t, content, "debug message", "debug level should log debug messages")
	assert.Contains(t, content, "info message")
	assert.Contains(t, content, "warn message")
	assert.Contains(t, content, "error message")
}

func TestInitLoggerLevels_Info(t *testing.T) {
	saveAndRestoreLogger(t)
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	InitLogger("info", "text", logPath)
	Debug("debug message")
	Info("info message")
	Warn("warn message")
	Error("error message")

	content := readLogFile(t, logPath)
	assert.NotContains(t, content, "debug message", "info level should suppress debug messages")
	assert.Contains(t, content, "info message")
	assert.Contains(t, content, "warn message")
	assert.Contains(t, content, "error message")
}

func TestInitLoggerLevels_Warn(t *testing.T) {
	saveAndRestoreLogger(t)
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	InitLogger("warn", "text", logPath)
	Debug("debug message")
	Info("info message")
	Warn("warn message")
	Error("error message")

	content := readLogFile(t, logPath)
	assert.NotContains(t, content, "debug message")
	assert.NotContains(t, content, "info message", "warn level should suppress info messages")
	assert.Contains(t, content, "warn message")
	assert.Contains(t, content, "error message")
}

func TestInitLoggerLevels_Error(t *testing.T) {
	saveAndRestoreLogger(t)
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	InitLogger("error", "text", logPath)
	Debug("debug message")
	Info("info message")
	Warn("warn message")
	Error("error message")

	content := readLogFile(t, logPath)
	assert.NotContains(t, content, "debug message")
	assert.NotContains(t, content, "info message")
	assert.NotContains(t, content, "warn message", "error level should suppress warn messages")
	assert.Contains(t, content, "error message")
}

func TestInitLoggerLevels_Default(t *testing.T) {
	saveAndRestoreLogger(t)
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	// Unknown level string should default to "info"
	InitLogger("unknown", "text", logPath)
	Debug("debug message")
	Info("info message")

	content := readLogFile(t, logPath)
	assert.NotContains(t, content, "debug message", "unknown level should default to info, suppressing debug")
	assert.Contains(t, content, "info message")
}

// ── Format Tests ─────────────────────────────────────────────────────────────

func TestLoggerFormats_Text(t *testing.T) {
	saveAndRestoreLogger(t)
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	InitLogger("info", "text", logPath)
	Info("text format message", "user", "alice")

	content := readLogFile(t, logPath)
	assert.Contains(t, content, "text format message", "should contain message")
	assert.Contains(t, content, "user=alice", "text format should use key=value style")
	assert.NotContains(t, content, "{", "text format should not be JSON")
}

func TestLoggerFormats_JSON(t *testing.T) {
	saveAndRestoreLogger(t)
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	InitLogger("info", "json", logPath)
	Info("json format message", "user", "bob")

	content := readLogFile(t, logPath)
	// Each line should be valid JSON
	lines := strings.Split(strings.TrimSpace(content), "\n")
	assert.GreaterOrEqual(t, len(lines), 1, "should have at least one log line")

	var entry map[string]interface{}
	err := json.Unmarshal([]byte(lines[0]), &entry)
	assert.NoError(t, err, "log line should be valid JSON")
	assert.Equal(t, "json format message", entry["msg"])
	assert.Equal(t, "INFO", entry["level"])
}

func TestLoggerFormats_DefaultIsText(t *testing.T) {
	saveAndRestoreLogger(t)
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	// Unknown format should default to "text"
	InitLogger("info", "unknown", logPath)
	Info("default format message", "user", "charlie")

	content := readLogFile(t, logPath)
	assert.Contains(t, content, "default format message")
	assert.Contains(t, content, "user=charlie", "unknown format should default to text (key=value)")
}

// ── File Output Tests ────────────────────────────────────────────────────────

func TestLogToFile(t *testing.T) {
	saveAndRestoreLogger(t)
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "sote.log")

	InitLogger("info", "text", logPath)
	Info("file output test")

	content := readLogFile(t, logPath)
	assert.Contains(t, content, "file output test", "log message should be in file")
}

func TestLogToFile_CreatesParentDirs(t *testing.T) {
	saveAndRestoreLogger(t)
	tmpDir := t.TempDir()
	// Use a nested path that doesn't exist yet
	nestedPath := filepath.Join(tmpDir, "logs", "sub", "deep", "app.log")

	InitLogger("info", "text", nestedPath)
	Info("nested dir test")

	// Verify file exists and contains the message
	content := readLogFile(t, nestedPath)
	assert.Contains(t, content, "nested dir test", "log message should be in nested file")

	// Verify parent directories exist
	assert.DirExists(t, filepath.Dir(nestedPath), "parent directories should be created")
}

func TestLogToFile_Appends(t *testing.T) {
	saveAndRestoreLogger(t)
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "append.log")

	// Write initial content, then init logger and write again
	require.NoError(t, os.WriteFile(logPath, []byte("existing content\n"), 0644))

	InitLogger("info", "text", logPath)
	Info("appended message")

	content := readLogFile(t, logPath)
	assert.Contains(t, content, "existing content", "existing content should be preserved")
	assert.Contains(t, content, "appended message", "new message should be appended")
}

// ── Redaction Tests ──────────────────────────────────────────────────────────

func TestNoSecretLeakage_Password(t *testing.T) {
	saveAndRestoreLogger(t)
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	InitLogger("debug", "text", logPath)
	Info("login attempt", "username", "alice", "password", "supersecret123")

	content := readLogFile(t, logPath)
	assert.NotContains(t, content, "supersecret123", "password value must not appear in logs")
	assert.Contains(t, content, "REDACTED", "password value should be redacted")
	assert.Contains(t, content, "login attempt", "message should still be logged")
	assert.Contains(t, content, "username=alice", "non-sensitive fields should still appear")
}

func TestNoSecretLeakage_Token(t *testing.T) {
	saveAndRestoreLogger(t)
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	InitLogger("debug", "text", logPath)
	Info("api call", "token", "abc123secret", "endpoint", "/users")

	content := readLogFile(t, logPath)
	assert.NotContains(t, content, "abc123secret", "token value must not appear in logs")
	assert.Contains(t, content, "REDACTED", "token value should be redacted")
	assert.Contains(t, content, "endpoint=/users", "non-sensitive fields should appear")
}

func TestNoSecretLeakage_SecretKey(t *testing.T) {
	saveAndRestoreLogger(t)
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	InitLogger("debug", "text", logPath)
	Info("key loaded", "private_key", "---BEGIN RSA PRIVATE KEY---")

	content := readLogFile(t, logPath)
	assert.NotContains(t, content, "BEGIN RSA PRIVATE KEY", "private key must not appear in logs")
}

func TestNoSecretLeakage_CaseInsensitive(t *testing.T) {
	saveAndRestoreLogger(t)
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	InitLogger("debug", "text", logPath)
	// Test that redaction is case-insensitive
	Info("auth check", "Password", "MixedCase123")

	content := readLogFile(t, logPath)
	assert.NotContains(t, content, "MixedCase123", "case-insensitive redaction should work")
}

func TestNoSecretLeakage_JSONFormat(t *testing.T) {
	saveAndRestoreLogger(t)
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.json")

	InitLogger("debug", "json", logPath)
	Info("login attempt", "username", "alice", "password", "secret123")

	content := readLogFile(t, logPath)
	assert.NotContains(t, content, "secret123", "password must not appear in JSON logs")
	assert.Contains(t, content, "REDACTED", "password should be redacted in JSON logs")
}

// ── Edge Case Tests ──────────────────────────────────────────────────────────

func TestInitLogger_EmptyPath(t *testing.T) {
	saveAndRestoreLogger(t)

	// Empty path should use stdout — just verify it doesn't panic
	l := InitLogger("info", "text", "")
	assert.NotNil(t, l, "logger should not be nil with empty path")
}

func TestInitLogger_InvalidPath(t *testing.T) {
	saveAndRestoreLogger(t)

	// Path where we can't write (e.g., /proc is read-only on Linux)
	// This should fall back to stderr gracefully
	l := InitLogger("info", "text", "/proc/invalid/log/path/test.log")
	assert.NotNil(t, l, "logger should not be nil with invalid path")
}

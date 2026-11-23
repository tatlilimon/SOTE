package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

var logger *slog.Logger

// sensitiveKeys are log attribute keys whose values are redacted.
var sensitiveKeys = map[string]bool{
	"password":      true,
	"passwd":        true,
	"secret":        true,
	"private_key":   true,
	"privkey":       true,
	"token":         true,
	"api_key":       true,
	"apikey":        true,
	"auth":          true,
	"authorization": true,
}

// redactingHandler wraps a slog.Handler and redacts sensitive attribute values.
type redactingHandler struct {
	handler slog.Handler
}

func (h *redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *redactingHandler) Handle(ctx context.Context, r slog.Record) error {
	redacted := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		if sensitiveKeys[strings.ToLower(a.Key)] {
			redacted.AddAttrs(slog.String(a.Key, "[REDACTED]"))
		} else {
			redacted.AddAttrs(a)
		}
		return true
	})
	return h.handler.Handle(ctx, redacted)
}

func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &redactingHandler{handler: h.handler.WithAttrs(attrs)}
}

func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{handler: h.handler.WithGroup(name)}
}

// InitLogger initializes the structured logger with the given level, format,
// and output path. If logPath is empty, logs go to stdout. If the log file
// cannot be opened, logs fall back to stderr. Parent directories are created
// automatically.
func InitLogger(level, format, logPath string) *slog.Logger {
	lvl := parseLevel(level)

	w := openWriter(logPath)

	opts := &slog.HandlerOptions{Level: lvl}

	var baseHandler slog.Handler
	switch strings.ToLower(format) {
	case "json":
		baseHandler = slog.NewJSONHandler(w, opts)
	default:
		baseHandler = slog.NewTextHandler(w, opts)
	}

	redacted := &redactingHandler{handler: baseHandler}
	logger = slog.New(redacted)
	slog.SetDefault(logger)
	return logger
}

// parseLevel converts a level string to a slog.Level. Unknown values default
// to LevelInfo.
func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// openWriter returns an io.Writer for the given path. If path is empty,
// stdout is used. Parent directories are created as needed. On failure,
// the function logs to stderr and returns stderr.
func openWriter(logPath string) io.Writer {
	if logPath == "" {
		return os.Stdout
	}

	// Owner-only: the log records which onion addresses and contacts the user
	// talks to and when, which is sensitive metadata for this application.
	dir := filepath.Dir(logPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "logging: failed to create log directory %s: %v\n", dir, err)
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err == nil {
		_ = f.Chmod(0o600) // tighten a file created by an older version
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "logging: failed to open log file %s: %v, falling back to stderr\n", logPath, err)
		return os.Stderr
	}
	return f
}

// ── Package-level logging helpers ────────────────────────────────────────────

func Info(msg string, args ...any)  { logger.Info(msg, args...) }
func Debug(msg string, args ...any) { logger.Debug(msg, args...) }
func Warn(msg string, args ...any)  { logger.Warn(msg, args...) }
func Error(msg string, args ...any) { logger.Error(msg, args...) }

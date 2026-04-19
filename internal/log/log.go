// Package log provides structured logging for Mini Krill using Go's stdlib slog.
// Zero external dependencies. Logs to stderr and optionally to a file.
// Krill fact: krill navigate the dark ocean depths - this logger lights the way.
package log

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/srvsngh99/mini-krill/internal/config"
)

var logger *slog.Logger

func init() {
	// Default logger until Init() is called
	logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// Init configures the logger from config. Call once at startup.
func Init(cfg config.LogConfig) error {
	return initLogger(cfg, false)
}

// InitQuiet configures file-only logging (no stderr). Use for interactive modes
// like chat and TUI where log spam ruins the experience.
func InitQuiet(cfg config.LogConfig) error {
	return initLogger(cfg, true)
}

func initLogger(cfg config.LogConfig, quiet bool) error {
	level := parseLevel(cfg.Level)
	var writers []io.Writer

	if !quiet {
		writers = append(writers, os.Stderr)
	}

	if cfg.File != "" {
		dir := filepath.Dir(cfg.File)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create log dir: %w", err)
		}
		f, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		writers = append(writers, f)
	}

	// If no writers at all (quiet + no file), write to discard
	if len(writers) == 0 {
		writers = append(writers, io.Discard)
	}

	w := io.MultiWriter(writers...)
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if cfg.JSON {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		handler = slog.NewTextHandler(w, opts)
	}

	logger = slog.New(handler)
	slog.SetDefault(logger)
	return nil
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Convenience wrappers

func Debug(msg string, args ...any) { logger.Debug(msg, args...) }
func Info(msg string, args ...any)  { logger.Info(msg, args...) }
func Warn(msg string, args ...any)  { logger.Warn(msg, args...) }
func Error(msg string, args ...any) { logger.Error(msg, args...) }

// With returns a child logger with additional context fields.
func With(args ...any) *slog.Logger { return logger.With(args...) }

// Get returns the underlying slog.Logger.
func Get() *slog.Logger { return logger }

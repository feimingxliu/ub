// Package logx configures process-wide structured logging.
package logx

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// SetupFromEnv configures slog from UB_LOG_LEVEL and UB_LOG_FILE.
func SetupFromEnv(stderr io.Writer) (*slog.Logger, func() error, error) {
	logger, cleanup, _, err := setupFromEnv(stderr, "")
	return logger, cleanup, err
}

// SetupTUIFromEnv configures slog for the TUI. When UB_LOG_FILE is unset, logs
// are written to the default user-state log file so they do not corrupt the TUI.
func SetupTUIFromEnv(stderr io.Writer) (*slog.Logger, func() error, string, error) {
	defaultPath := ""
	if strings.TrimSpace(os.Getenv("UB_LOG_FILE")) == "" {
		path, err := DefaultFilePath()
		if err != nil {
			return nil, nil, "", err
		}
		defaultPath = path
	}
	return setupFromEnv(stderr, defaultPath)
}

// DefaultFilePath returns the default user-level log file path.
func DefaultFilePath() (string, error) {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "ub", "ub.log"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "ub", "ub.log"), nil
}

func setupFromEnv(stderr io.Writer, defaultFile string) (*slog.Logger, func() error, string, error) {
	level, err := ParseLevel(os.Getenv("UB_LOG_LEVEL"))
	if err != nil {
		return nil, nil, "", err
	}

	opts := &slog.HandlerOptions{Level: level}
	closeOutput := func() error { return nil }
	var handler slog.Handler
	logFile := strings.TrimSpace(os.Getenv("UB_LOG_FILE"))
	if logFile == "" {
		logFile = defaultFile
	}

	if logFile != "" {
		if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
			return nil, nil, "", fmt.Errorf("create log directory for %s: %w", logFile, err)
		}
		file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return nil, nil, "", fmt.Errorf("open log file %s: %w", logFile, err)
		}
		handler = slog.NewJSONHandler(file, opts)
		closeOutput = file.Close
	} else {
		handler = slog.NewTextHandler(stderr, opts)
	}

	previous := slog.Default()
	logger := slog.New(handler)
	slog.SetDefault(logger)
	cleanup := func() error {
		slog.SetDefault(previous)
		return closeOutput()
	}
	return logger, cleanup, logFile, nil
}

// ParseLevel parses UB_LOG_LEVEL values.
func ParseLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("invalid UB_LOG_LEVEL %q (expected debug, info, warn, or error)", raw)
	}
}

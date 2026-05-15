// Package logx configures process-wide structured logging.
package logx

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// SetupFromEnv configures slog from UB_LOG_LEVEL and UB_LOG_FILE.
func SetupFromEnv(stderr io.Writer) (*slog.Logger, func() error, error) {
	level, err := ParseLevel(os.Getenv("UB_LOG_LEVEL"))
	if err != nil {
		return nil, nil, err
	}

	opts := &slog.HandlerOptions{Level: level}
	closeOutput := func() error { return nil }
	var handler slog.Handler

	if path := os.Getenv("UB_LOG_FILE"); path != "" {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return nil, nil, fmt.Errorf("open UB_LOG_FILE %s: %w", path, err)
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
	return logger, cleanup, nil
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

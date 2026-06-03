// Package logx configures process-wide structured logging.
package logx

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/feimingxliu/ub/internal/paths"
)

// RotationOptions controls best-effort file rotation before a log file is
// opened. Non-positive MaxSizeBytes or negative MaxBackups disables rotation.
type RotationOptions struct {
	MaxSizeBytes int64
	MaxBackups   int
}

// SetupFromEnv configures slog from UB_LOG_LEVEL and UB_LOG_FILE.
func SetupFromEnv(stderr io.Writer) (*slog.Logger, func() error, error) {
	logger, cleanup, _, err := setupFromEnv(stderr, "", RotationOptions{})
	return logger, cleanup, err
}

// SetupFromEnvWithRotation configures slog and rotates UB_LOG_FILE first when
// the configured threshold has been exceeded.
func SetupFromEnvWithRotation(stderr io.Writer, rotation RotationOptions) (*slog.Logger, func() error, error) {
	logger, cleanup, _, err := setupFromEnv(stderr, "", rotation)
	return logger, cleanup, err
}

// SetupTUIFromEnv configures slog for the TUI. When UB_LOG_FILE is unset, logs
// are written to the default user-state log file so they do not corrupt the TUI.
func SetupTUIFromEnv(stderr io.Writer) (*slog.Logger, func() error, string, error) {
	return SetupTUIFromEnvWithRotation(stderr, RotationOptions{})
}

// SetupTUIFromEnvWithRotation configures TUI logging and rotates the selected
// file first when rotation is enabled.
func SetupTUIFromEnvWithRotation(stderr io.Writer, rotation RotationOptions) (*slog.Logger, func() error, string, error) {
	defaultPath := ""
	if strings.TrimSpace(os.Getenv("UB_LOG_FILE")) == "" {
		path, err := DefaultFilePath()
		if err != nil {
			return nil, nil, "", err
		}
		defaultPath = path
	}
	return setupFromEnv(stderr, defaultPath, rotation)
}

// DefaultFilePath returns the default user-level log file path.
func DefaultFilePath() (string, error) {
	stateRoot, err := paths.StateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(stateRoot, "ub.log"), nil
}

func setupFromEnv(stderr io.Writer, defaultFile string, rotation RotationOptions) (*slog.Logger, func() error, string, error) {
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

	var rotationErr error
	if logFile != "" {
		if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
			return nil, nil, "", fmt.Errorf("create log directory for %s: %w", logFile, err)
		}
		rotationErr = rotateIfNeeded(logFile, rotation.MaxSizeBytes, rotation.MaxBackups)
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
	if rotationErr != nil {
		logger.Warn("rotate log file", "path", logFile, "err", rotationErr)
	}
	cleanup := func() error {
		slog.SetDefault(previous)
		return closeOutput()
	}
	return logger, cleanup, logFile, nil
}

func rotateIfNeeded(path string, maxSizeBytes int64, maxBackups int) error {
	if maxSizeBytes <= 0 || maxBackups < 0 {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Size() <= maxSizeBytes {
		return nil
	}
	if maxBackups == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove current log %s: %w", path, err)
		}
		return nil
	}
	oldest := backupPath(path, maxBackups)
	if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove oldest log %s: %w", oldest, err)
	}
	for i := maxBackups - 1; i >= 1; i-- {
		src := backupPath(path, i)
		dst := backupPath(path, i+1)
		if err := renameIfExists(src, dst); err != nil {
			return err
		}
	}
	if err := renameIfExists(path, backupPath(path, 1)); err != nil {
		return err
	}
	return nil
}

func backupPath(path string, n int) string {
	return fmt.Sprintf("%s.%d", path, n)
}

func renameIfExists(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", src, err)
	}
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove log backup %s: %w", dst, err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("rotate log %s to %s: %w", src, dst, err)
	}
	return nil
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

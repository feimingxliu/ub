package logx

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"":        slog.LevelInfo,
		"info":    slog.LevelInfo,
		"debug":   slog.LevelDebug,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
	}
	for raw, want := range cases {
		got, err := ParseLevel(raw)
		if err != nil {
			t.Fatalf("ParseLevel(%q): %v", raw, err)
		}
		if got != want {
			t.Fatalf("ParseLevel(%q) = %v, want %v", raw, got, want)
		}
	}
	if _, err := ParseLevel("verbose"); err == nil || !strings.Contains(err.Error(), "invalid UB_LOG_LEVEL") {
		t.Fatalf("ParseLevel(verbose) err = %v, want invalid UB_LOG_LEVEL", err)
	}
}

func TestSetupFromEnvDefaultInfoLevel(t *testing.T) {
	t.Setenv("UB_LOG_LEVEL", "")
	t.Setenv("UB_LOG_FILE", "")
	stderr := &bytes.Buffer{}
	logger, cleanup, err := SetupFromEnv(stderr)
	if err != nil {
		t.Fatalf("SetupFromEnv: %v", err)
	}
	defer cleanup()

	if logger.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("debug should be disabled by default")
	}
	logger.Info("visible")
	logger.Debug("hidden")
	if got := stderr.String(); !strings.Contains(got, "visible") || strings.Contains(got, "hidden") {
		t.Fatalf("unexpected stderr logs:\n%s", got)
	}
}

func TestSetupFromEnvDebugLevel(t *testing.T) {
	t.Setenv("UB_LOG_LEVEL", "debug")
	t.Setenv("UB_LOG_FILE", "")
	stderr := &bytes.Buffer{}
	logger, cleanup, err := SetupFromEnv(stderr)
	if err != nil {
		t.Fatalf("SetupFromEnv: %v", err)
	}
	defer cleanup()

	logger.Debug("debug visible")
	if !strings.Contains(strings.ToLower(stderr.String()), "debug") {
		t.Fatalf("debug log missing from stderr:\n%s", stderr.String())
	}
}

func TestSetupFromEnvInvalidLevel(t *testing.T) {
	t.Setenv("UB_LOG_LEVEL", "verbose")
	t.Setenv("UB_LOG_FILE", "")
	if _, _, err := SetupFromEnv(&bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "invalid UB_LOG_LEVEL") {
		t.Fatalf("SetupFromEnv err = %v, want invalid UB_LOG_LEVEL", err)
	}
}

func TestSetupFromEnvLogFileWritesJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ub.log")
	t.Setenv("UB_LOG_LEVEL", "info")
	t.Setenv("UB_LOG_FILE", path)
	stderr := &bytes.Buffer{}
	logger, cleanup, err := SetupFromEnv(stderr)
	if err != nil {
		t.Fatalf("SetupFromEnv: %v", err)
	}
	logger.Info("file only", "key", "value")
	if err := cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr got structured logs: %s", stderr.String())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(raw), &decoded); err != nil {
		t.Fatalf("log file is not JSON:\n%s\nerr: %v", string(raw), err)
	}
	for _, key := range []string{"time", "level", "msg"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("log JSON missing key %q: %#v", key, decoded)
		}
	}
}

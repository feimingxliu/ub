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

func TestSetupFromEnvRotatesOversizedLogFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ub.log")
	if err := os.WriteFile(path, []byte("old log contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UB_LOG_LEVEL", "info")
	t.Setenv("UB_LOG_FILE", path)

	logger, cleanup, err := SetupFromEnvWithRotation(&bytes.Buffer{}, RotationOptions{
		MaxSizeBytes: 4,
		MaxBackups:   2,
	})
	if err != nil {
		t.Fatalf("SetupFromEnvWithRotation: %v", err)
	}
	logger.Info("new log")
	if err := cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	backup, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backup) != "old log contents" {
		t.Fatalf("backup = %q", string(backup))
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read current log: %v", err)
	}
	if !bytes.Contains(current, []byte("new log")) || bytes.Contains(current, []byte("old log contents")) {
		t.Fatalf("current log after rotation:\n%s", string(current))
	}
}

func TestRotateIfNeededShiftsBackupsAndDeletesOldest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ub.log")
	files := map[string]string{
		path:        "current",
		path + ".1": "one",
		path + ".2": "two",
	}
	for name, content := range files {
		if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := rotateIfNeeded(path, 1, 2); err != nil {
		t.Fatalf("rotateIfNeeded: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("current log should be absent until reopened, stat err = %v", err)
	}
	assertFileContent(t, path+".1", "current")
	assertFileContent(t, path+".2", "one")
	if raw, err := os.ReadFile(path + ".3"); err == nil {
		t.Fatalf("unexpected extra backup: %q", string(raw))
	}
}

func TestRotateIfNeededDisabledBySize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ub.log")
	if err := os.WriteFile(path, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rotateIfNeeded(path, 0, 2); err != nil {
		t.Fatalf("rotateIfNeeded disabled: %v", err)
	}
	assertFileContent(t, path, "current")
}

func TestDefaultFilePathUsesXDGStateHome(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	got, err := DefaultFilePath()
	if err != nil {
		t.Fatalf("DefaultFilePath: %v", err)
	}
	want := filepath.Join(state, "ub", "ub.log")
	if got != want {
		t.Fatalf("DefaultFilePath() = %q, want %q", got, want)
	}
}

func TestSetupTUIFromEnvWritesDefaultLogFile(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("UB_LOG_LEVEL", "info")
	t.Setenv("UB_LOG_FILE", "")

	stderr := &bytes.Buffer{}
	logger, cleanup, path, err := SetupTUIFromEnv(stderr)
	if err != nil {
		t.Fatalf("SetupTUIFromEnv: %v", err)
	}
	logger.Info("tui file only", "key", "value")
	if err := cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr got structured logs: %s", stderr.String())
	}
	wantPath := filepath.Join(state, "ub", "ub.log")
	if path != wantPath {
		t.Fatalf("path = %q, want %q", path, wantPath)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !bytes.Contains(raw, []byte("tui file only")) {
		t.Fatalf("log file missing message:\n%s", string(raw))
	}
}

func TestSetupTUIFromEnvRotatesDefaultLogFile(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("UB_LOG_LEVEL", "info")
	t.Setenv("UB_LOG_FILE", "")

	path := filepath.Join(state, "ub", "ub.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old tui log"), 0o600); err != nil {
		t.Fatal(err)
	}
	logger, cleanup, gotPath, err := SetupTUIFromEnvWithRotation(&bytes.Buffer{}, RotationOptions{
		MaxSizeBytes: 1,
		MaxBackups:   1,
	})
	if err != nil {
		t.Fatalf("SetupTUIFromEnvWithRotation: %v", err)
	}
	logger.Info("new tui log")
	if err := cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if gotPath != path {
		t.Fatalf("path = %q, want %q", gotPath, path)
	}
	assertFileContent(t, path+".1", "old tui log")
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read current log: %v", err)
	}
	if !bytes.Contains(current, []byte("new tui log")) {
		t.Fatalf("current TUI log missing new entry:\n%s", string(current))
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(raw) != want {
		t.Fatalf("%s = %q, want %q", path, string(raw), want)
	}
}

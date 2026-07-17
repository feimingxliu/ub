package eval

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestLoadTaskAndPrepareFixture(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join(dir, "fixture")
	if err := os.Mkdir(fixture, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(dir, "task.yaml")
	writeTask(t, taskPath, `schema_version: 1
name: load-fixture
prompt: fix it
fixture: fixture
timeout: 30s
assertions:
  files:
    - path: main.go
      contains: ["package main"]
`)
	taskFile, err := LoadTask(taskPath)
	if err != nil {
		t.Fatalf("LoadTask: %v", err)
	}
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := PrepareFixture(taskFile, workspace); err != nil {
		t.Fatalf("PrepareFixture: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(workspace, "main.go"))
	if err != nil || string(data) != "package main\n" {
		t.Fatalf("copied fixture = %q, err=%v", data, err)
	}
}

func TestMVPEvalTasksLoad(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Join(filepath.Dir(current), "..", "..", "docs", "eval-tasks")
	paths, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 5 {
		t.Fatalf("MVP task count = %d, want 5: %v", len(paths), paths)
	}
	var names []string
	for _, path := range paths {
		taskFile, err := LoadTask(path)
		if err != nil {
			t.Errorf("LoadTask(%s): %v", filepath.Base(path), err)
			continue
		}
		names = append(names, taskFile.Task.Name)
		workspace := filepath.Join(t.TempDir(), "workspace")
		if err := os.Mkdir(workspace, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := PrepareFixture(taskFile, workspace); err != nil {
			t.Errorf("PrepareFixture(%s): %v", taskFile.Task.Name, err)
		}
	}
	sort.Strings(names)
	for i := 1; i < len(names); i++ {
		if names[i] == names[i-1] {
			t.Errorf("duplicate task name %q", names[i])
		}
	}
}

func TestLoadTaskRejectsUnsafePaths(t *testing.T) {
	dir := t.TempDir()
	for name, path := range map[string]string{"absolute": filepath.Join(dir, "escape"), "parent": "../escape"} {
		t.Run(name, func(t *testing.T) {
			taskPath := filepath.Join(t.TempDir(), "task.yaml")
			writeTask(t, taskPath, "schema_version: 1\nname: unsafe\nprompt: no\nfixture: "+path+"\nassertions:\n  rollout:\n    tools_called: [read]\n")
			if _, err := LoadTask(taskPath); err == nil {
				t.Fatal("LoadTask succeeded, want unsafe path error")
			}
		})
	}
}

func TestPrepareFixtureRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}
	dir := t.TempDir()
	fixture := filepath.Join(dir, "fixture")
	if err := os.Mkdir(fixture, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(fixture, "link")); err != nil {
		t.Fatal(err)
	}
	task := Task{SchemaVersion: 1, Name: "symlink", Prompt: "x", Fixture: "fixture", Assertions: Assertions{Rollout: RolloutAssertions{ToolsCalled: []string{"read"}}}}
	err := PrepareFixture(TaskFile{Task: task, Dir: dir}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("PrepareFixture error = %v, want symlink", err)
	}
}

func TestResolveTaskPathByName(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "docs", "eval-tasks", "sample.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTask(t, path, "schema_version: 1\nname: sample\nprompt: x\nassertions:\n  rollout:\n    tools_called: [read]\n")
	got, err := ResolveTaskPath(workspace, "sample")
	if err != nil {
		t.Fatalf("ResolveTaskPath: %v", err)
	}
	if got != path {
		t.Fatalf("path = %q, want %q", got, path)
	}
}

func writeTask(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

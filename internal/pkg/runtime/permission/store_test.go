package permission

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProjectRulesMissing(t *testing.T) {
	allow, ask, deny, err := LoadProjectRules(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("LoadProjectRules: %v", err)
	}
	if len(allow) != 0 || len(ask) != 0 || len(deny) != 0 {
		t.Fatalf("rules = allow:%#v ask:%#v deny:%#v, want empty", allow, ask, deny)
	}
}

func TestSaveAndLoadProjectRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "work", ".ub", "permissions.yaml")
	if err := SaveProjectRule(path, RuleAllow, "Bash(go test ./...)"); err != nil {
		t.Fatalf("SaveProjectRule allow: %v", err)
	}
	if err := SaveProjectRule(path, RuleAllow, "Bash(go test:*)"); err != nil {
		t.Fatalf("SaveProjectRule pattern: %v", err)
	}
	if err := SaveProjectRule(path, RuleAsk, "Bash(git push:*)"); err != nil {
		t.Fatalf("SaveProjectRule ask: %v", err)
	}
	if err := SaveProjectRule(path, RuleDeny, "Bash(curl:*)"); err != nil {
		t.Fatalf("SaveProjectRule deny: %v", err)
	}
	allow, ask, deny, err := LoadProjectRules(path)
	if err != nil {
		t.Fatalf("LoadProjectRules: %v", err)
	}
	if len(allow) != 2 || allow[0].Raw != "Bash(go test ./...)" || allow[1].Pattern != "go test:*" {
		t.Fatalf("allow = %#v, want exact and pattern rules", allow)
	}
	if len(ask) != 1 || ask[0].Raw != "Bash(git push:*)" {
		t.Fatalf("ask = %#v, want ask rule", ask)
	}
	if len(deny) != 1 || deny[0].Raw != "Bash(curl:*)" {
		t.Fatalf("deny = %#v, want deny rule", deny)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read project rules: %v", err)
	}
	for _, want := range []string{"permissions:", "allow:", "ask:", "deny:"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("project rules file missing %q:\n%s", want, raw)
		}
	}
	if strings.Contains(string(raw), "global:") || strings.Contains(string(raw), "project:") {
		t.Fatalf("project rules file kept legacy keys:\n%s", raw)
	}
}

func TestWriteRuleFileAtomicFailurePreservesOriginal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "permissions.yaml")
	original := []byte("permissions:\n  allow:\n  - Bash(go test:*)\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatalf("write original: %v", err)
	}
	want := errors.New("stop before rename")
	err := writeRuleFileAtomic(path, ruleFile{Permissions: RuleConfig{Deny: []string{"Bash(curl:*)"}}}, func(tmp string) error {
		if _, statErr := os.Stat(tmp); statErr != nil {
			t.Fatalf("temp file missing before rename: %v", statErr)
		}
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("writeRuleFileAtomic err = %v, want %v", err, want)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read original: %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("file changed to %q, want %q", got, original)
	}
	if _, _, _, err := LoadProjectRules(path); err != nil {
		t.Fatalf("LoadProjectRules after failed write: %v", err)
	}
}

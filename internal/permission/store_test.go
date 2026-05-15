package permission

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadGlobalRulesMissing(t *testing.T) {
	rules, err := LoadGlobalRules(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("LoadGlobalRules: %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("rules = %#v, want empty", rules)
	}
}

func TestSaveAndLoadGlobalRule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ub", "permissions.yaml")
	if err := SaveGlobalRule(path, Rule{Tool: "bash"}); err != nil {
		t.Fatalf("SaveGlobalRule: %v", err)
	}
	if err := SaveGlobalRule(path, Rule{Tool: "bash", CommandPrefix: "git "}); err != nil {
		t.Fatalf("SaveGlobalRule second: %v", err)
	}
	rules, err := LoadGlobalRules(path)
	if err != nil {
		t.Fatalf("LoadGlobalRules: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("rules len = %d, want 2: %#v", len(rules), rules)
	}
	if rules[0].Tool != "bash" || rules[1].CommandPrefix != "git " {
		t.Fatalf("rules = %#v", rules)
	}
}

func TestWriteRuleFileAtomicFailurePreservesOriginal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "permissions.yaml")
	original := []byte("global:\n- tool: bash\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatalf("write original: %v", err)
	}
	want := errors.New("stop before rename")
	err := writeRuleFileAtomic(path, ruleFile{Global: []Rule{{Tool: "job_run"}}}, func(tmp string) error {
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
	if _, err := LoadGlobalRules(path); err != nil {
		t.Fatalf("LoadGlobalRules after failed write: %v", err)
	}
}

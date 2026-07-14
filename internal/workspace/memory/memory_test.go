package memory

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func freezeTime(t *testing.T, instant time.Time) {
	t.Helper()
	orig := nowFunc
	nowFunc = func() time.Time { return instant }
	t.Cleanup(func() { nowFunc = orig })
}

// --- Scope & Category validation ---

func TestValidScope(t *testing.T) {
	for _, s := range []string{"auto", "global", "workspace"} {
		if !ValidScope(s) {
			t.Errorf("ValidScope(%q) = false", s)
		}
	}
	for _, s := range []string{"", "session", "nope"} {
		if ValidScope(s) {
			t.Errorf("ValidScope(%q) = true", s)
		}
	}
}

func TestValidCategory(t *testing.T) {
	for _, c := range []string{"preference", "project", "pattern", "decision", "debug", "general"} {
		if !ValidCategory(c) {
			t.Errorf("ValidCategory(%q) = false", c)
		}
	}
	for _, c := range []string{"", "nope", "workspace"} {
		if ValidCategory(c) {
			t.Errorf("ValidCategory(%q) = true", c)
		}
	}
}

// --- Path ---

func TestPath_AutoRequiresRoot(t *testing.T) {
	if _, err := Path("", ScopeAuto); err == nil {
		t.Fatalf("expected error for empty workspace root")
	}
}

func TestPath_AutoUnderStateRoot(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	ws := t.TempDir()
	p, err := Path(ws, ScopeAuto)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if !strings.Contains(p, "memory") || !strings.HasSuffix(p, "memory.md") {
		t.Fatalf("auto path = %s, expected under .../memory/<key>/memory.md", p)
	}
}

func TestPath_WorkspaceAliasResolvesToAuto(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	ws := t.TempDir()
	p1, err := Path(ws, ScopeWorkspace)
	if err != nil {
		t.Fatalf("Path workspace: %v", err)
	}
	p2, err := Path(ws, ScopeAuto)
	if err != nil {
		t.Fatalf("Path auto: %v", err)
	}
	if p1 != p2 {
		t.Fatalf("workspace scope should resolve to same path as auto: %q != %q", p1, p2)
	}
}

func TestPath_GlobalUsesConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/cfg")
	p, err := Path("", ScopeGlobal)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if p != filepath.Join("/tmp/cfg", "ub", "instructions.md") {
		t.Fatalf("global path = %s", p)
	}
}

// --- Append ---

func TestAppend_CreatesFileWithCategory(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	ws := t.TempDir()
	freezeTime(t, time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC))

	path, _, err := Append(ws, ScopeAuto, CatProject, "build is `make build`")
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if path == "" {
		t.Fatalf("expected non-empty path")
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(body)
	if !strings.Contains(content, "## project") {
		t.Fatalf("missing category heading:\n%s", content)
	}
	if !strings.Contains(content, "build is `make build`") {
		t.Fatalf("missing text:\n%s", content)
	}
	if !strings.Contains(content, "*(2026-05-27T10:00:00Z)*") {
		t.Fatalf("missing timestamp:\n%s", content)
	}
}

func TestAppend_DeduplicatesSimilarEntries(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	ws := t.TempDir()
	freezeTime(t, time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC))

	if _, _, err := Append(ws, ScopeAuto, CatProject, "build is `make build`"); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	freezeTime(t, time.Date(2026, 5, 27, 11, 0, 0, 0, time.UTC))
	if _, _, err := Append(ws, ScopeAuto, CatProject, "build is make build"); err != nil {
		t.Fatalf("append 2: %v", err)
	}

	path, _ := Path(ws, ScopeAuto)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(body)
	// Should only have one entry (updated timestamp), not two.
	count := strings.Count(content, "make build")
	if count != 1 {
		t.Fatalf("expected 1 entry after dedup, found %d:\n%s", count, content)
	}
	if !strings.Contains(content, "*(2026-05-27T11:00:00Z)*") {
		t.Fatalf("timestamp should be updated:\n%s", content)
	}
}

func TestAppend_MergesSameTopicConflicts(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	ws := t.TempDir()
	freezeTime(t, time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC))

	if _, _, err := Append(ws, ScopeAuto, CatProject, "build command is `make build`"); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	freezeTime(t, time.Date(2026, 5, 27, 11, 0, 0, 0, time.UTC))
	out, err := AppendWithOutcome(ws, ScopeAuto, CatProject, "build uses `npm run build`")
	if err != nil {
		t.Fatalf("append 2: %v", err)
	}
	if out.Action != AppendActionMerged {
		t.Fatalf("action = %q, want merged", out.Action)
	}

	path, _ := Path(ws, ScopeAuto)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(body)
	if strings.Contains(content, "make build") || !strings.Contains(content, "npm run build") {
		t.Fatalf("same-topic conflict should replace old fact:\n%s", content)
	}
}

func TestAppend_DoesNotDeduplicateDistinctUnicodeEntries(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	ws := t.TempDir()
	freezeTime(t, time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC))

	first := "使用前先阅读需求文档"
	second := "提交前运行全部测试"
	if _, _, err := Append(ws, ScopeAuto, CatProject, first); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if _, _, err := Append(ws, ScopeAuto, CatProject, second); err != nil {
		t.Fatalf("append 2: %v", err)
	}

	path, _ := Path(ws, ScopeAuto)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(body)
	if !strings.Contains(content, first) || !strings.Contains(content, second) {
		t.Fatalf("distinct unicode entries should both remain:\n%s", content)
	}
}

func TestAppend_DifferentCategoriesNoDedup(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	ws := t.TempDir()
	freezeTime(t, time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC))

	if _, _, err := Append(ws, ScopeAuto, CatProject, "use make to build"); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if _, _, err := Append(ws, ScopeAuto, CatPreference, "use make to build"); err != nil {
		t.Fatalf("append 2: %v", err)
	}

	path, _ := Path(ws, ScopeAuto)
	body, _ := os.ReadFile(path)
	content := string(body)
	// Two entries in different categories.
	count := strings.Count(content, "use make to build")
	if count != 2 {
		t.Fatalf("expected 2 entries in different categories, found %d:\n%s", count, content)
	}
}

func TestAppend_EmptyTextRejected(t *testing.T) {
	if _, _, err := Append(t.TempDir(), ScopeAuto, CatGeneral, "   "); err == nil {
		t.Fatalf("expected empty-text error")
	}
}

func TestAppend_InvalidCategoryRejected(t *testing.T) {
	if _, _, err := Append(t.TempDir(), ScopeAuto, Category("nope"), "text"); err == nil {
		t.Fatalf("expected invalid-category error")
	}
}

func TestForgetWithOutcome_RemovesExactAutoMemory(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	ws := t.TempDir()
	if _, _, err := Append(ws, ScopeAuto, CatProject, "build is make"); err != nil {
		t.Fatalf("seed project memory: %v", err)
	}
	if _, _, err := Append(ws, ScopeAuto, CatPreference, "prefer pnpm"); err != nil {
		t.Fatalf("seed preference memory: %v", err)
	}

	out, err := ForgetWithOutcome(ws, CatProject, "BUILD IS MAKE")
	if err != nil {
		t.Fatalf("ForgetWithOutcome: %v", err)
	}
	if out.Action != AppendActionDeleted || out.Scope != ScopeAuto || out.Category != CatProject {
		t.Fatalf("outcome = %#v", out)
	}
	got := Read(ws, 0)
	if strings.Contains(got, "build is make") || !strings.Contains(got, "prefer pnpm") {
		t.Fatalf("memory after forget =\n%s", got)
	}
}

func TestForgetWithOutcome_RequiresExactAutoMemoryEntry(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	ws := t.TempDir()
	if _, _, err := Append(ws, ScopeAuto, CatProject, "build is make"); err != nil {
		t.Fatalf("seed memory: %v", err)
	}
	if _, err := ForgetWithOutcome(ws, CatProject, "build"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("ForgetWithOutcome error = %v, want exact-match failure", err)
	}
}

func TestAutoMemoryMutationsSerializeAppendAndForget(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	ws := t.TempDir()
	if _, _, err := Append(ws, ScopeAuto, CatProject, "obsolete build command"); err != nil {
		t.Fatalf("seed auto memory: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	// Hold the shared mutation lock while both operations are launched. Once it
	// is released, each operation must re-read the other operation's completed
	// write before producing its own replacement file.
	autoMemoryMutationMu.Lock()
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := AppendWithOutcome(ws, ScopeAuto, CatPreference, "current build command")
		errs <- err
	}()
	go func() {
		defer wg.Done()
		_, err := ForgetWithOutcome(ws, CatProject, "obsolete build command")
		errs <- err
	}()
	autoMemoryMutationMu.Unlock()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent auto-memory mutation: %v", err)
		}
	}

	got := Read(ws, 0)
	if strings.Contains(got, "obsolete build command") || !strings.Contains(got, "current build command") {
		t.Fatalf("serialized mutations produced memory:\n%s", got)
	}
}

func TestAppend_GlobalInstructions(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "cfg"))
	ws := t.TempDir()
	freezeTime(t, time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC))

	path, _, err := Append(ws, ScopeGlobal, CatPreference, "prefer pnpm over npm")
	if err != nil {
		t.Fatalf("append global: %v", err)
	}
	if !strings.HasSuffix(path, "instructions.md") {
		t.Fatalf("global path = %s, expected instructions.md", path)
	}

	body, _ := os.ReadFile(path)
	content := string(body)
	if !strings.Contains(content, "prefer pnpm over npm") {
		t.Fatalf("missing text:\n%s", content)
	}
}

func TestAppend_GlobalPreservesManualInstructions(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "cfg")
	t.Setenv("XDG_CONFIG_HOME", cfg)
	ws := t.TempDir()
	path := filepath.Join(cfg, "ub", "instructions.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manual := "# User instructions\n\nPrefer concise engineering answers.\n"
	if err := os.WriteFile(path, []byte(manual), 0o644); err != nil {
		t.Fatalf("write manual instructions: %v", err)
	}

	if _, _, err := Append(ws, ScopeGlobal, CatPreference, "prefer pnpm over npm"); err != nil {
		t.Fatalf("append global: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(body)
	if !strings.Contains(content, manual) || !strings.Contains(content, "prefer pnpm over npm") {
		t.Fatalf("global append should preserve manual content and add entry:\n%s", content)
	}
}

func TestAppend_RejectsCredentialLikeMemory(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	ws := t.TempDir()
	if _, _, err := Append(ws, ScopeAuto, CatProject, "api_key=sk-test-secret-value"); err == nil || !strings.Contains(err.Error(), "privacy guard") {
		t.Fatalf("expected privacy rejection, got: %v", err)
	}
}

func TestAppend_DecaysOldDebugMemory(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	ws := t.TempDir()
	freezeTime(t, time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
	if _, _, err := Append(ws, ScopeAuto, CatDebug, "old reusable debug note"); err != nil {
		t.Fatalf("append debug: %v", err)
	}
	freezeTime(t, time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC))
	out, err := AppendWithOutcome(ws, ScopeAuto, CatProject, "build is `make build`")
	if err != nil {
		t.Fatalf("append project: %v", err)
	}
	if out.DroppedExpired != 1 {
		t.Fatalf("DroppedExpired = %d, want 1", out.DroppedExpired)
	}
	got := Read(ws, 0)
	if strings.Contains(got, "old reusable debug note") || !strings.Contains(got, "make build") {
		t.Fatalf("debug decay result =\n%s", got)
	}
}

// --- Read ---

func TestRead_MissingFilesReturnEmpty(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "cfg"))
	got := Read(t.TempDir(), 0)
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestRead_ConcatsGlobalThenAuto(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "cfg"))
	ws := t.TempDir()
	freezeTime(t, time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC))

	if _, _, err := Append(ws, ScopeGlobal, CatPreference, "global-fact"); err != nil {
		t.Fatalf("append global: %v", err)
	}
	if _, _, err := Append(ws, ScopeAuto, CatProject, "auto-fact"); err != nil {
		t.Fatalf("append auto: %v", err)
	}
	got := Read(ws, 0)
	if !strings.Contains(got, "<!-- global instructions -->") || !strings.Contains(got, "<!-- auto memory -->") {
		t.Fatalf("missing section markers:\n%s", got)
	}
	if strings.Index(got, "global-fact") > strings.Index(got, "auto-fact") {
		t.Fatalf("global must come before auto:\n%s", got)
	}
}

func TestRead_TruncatesByPriority(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "cfg"))
	ws := t.TempDir()
	freezeTime(t, time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC))

	// Add a high-priority preference and a low-priority debug entry.
	if _, _, err := Append(ws, ScopeAuto, CatDebug, strings.Repeat("d", 500)); err != nil {
		t.Fatalf("append debug: %v", err)
	}
	if _, _, err := Append(ws, ScopeAuto, CatPreference, "PREF-MARKER"); err != nil {
		t.Fatalf("append preference: %v", err)
	}

	got := Read(ws, 200)
	if !strings.Contains(got, "memory truncated") {
		t.Fatalf("missing truncation marker:\n%s", got)
	}
	if !strings.Contains(got, "PREF-MARKER") {
		t.Fatalf("preference should survive truncation:\n%s", got)
	}
}

// --- Recall ---

func TestRecall_ByQuery(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	ws := t.TempDir()
	freezeTime(t, time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC))

	if _, _, err := Append(ws, ScopeAuto, CatProject, "build is `make build`"); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, _, err := Append(ws, ScopeAuto, CatPreference, "prefer pnpm"); err != nil {
		t.Fatalf("append: %v", err)
	}

	results, err := Recall(ws, "build", "")
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Category != CatProject {
		t.Fatalf("expected project category, got %s", results[0].Category)
	}
}

func TestRecall_ByCategory(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	ws := t.TempDir()
	freezeTime(t, time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC))

	if _, _, err := Append(ws, ScopeAuto, CatProject, "build is make"); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, _, err := Append(ws, ScopeAuto, CatPreference, "prefer pnpm"); err != nil {
		t.Fatalf("append: %v", err)
	}

	results, err := Recall(ws, "", CatPreference)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Text != "prefer pnpm" {
		t.Fatalf("wrong result: %s", results[0].Text)
	}
}

func TestRecall_EmptyRoot(t *testing.T) {
	if _, err := Recall("", "query", ""); err == nil {
		t.Fatalf("expected error for empty root")
	}
}

// --- Parsing helpers ---

func TestParseEntries(t *testing.T) {
	input := `## preference

- prefer pnpm over npm *(2026-05-27T10:00:00Z)*
- always use conventional commits *(2026-05-28T09:00:00Z)*

## project

- build is ` + "`make build`" + ` *(2026-05-27T10:00:00Z)*

`
	entries := parseEntries(input)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Category != CatPreference {
		t.Fatalf("entry[0] category = %s, want preference", entries[0].Category)
	}
	if entries[0].Text != "prefer pnpm over npm" {
		t.Fatalf("entry[0] text = %q", entries[0].Text)
	}
	if entries[2].Category != CatProject {
		t.Fatalf("entry[2] category = %s, want project", entries[2].Category)
	}
}

// --- Similarity ---

func TestIsSimilar(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"build is make build", "build is `make build`", true},                      // containment
		{"prefer pnpm", "prefer pnpm over npm", true},                               // containment
		{"build is make", "test is go test", false},                                 // no overlap
		{"use errors.Is for error comparison", "use errors.Is instead of ==", true}, // high word overlap
	}
	for _, tt := range tests {
		if got := isSimilar(tt.a, tt.b); got != tt.want {
			t.Errorf("isSimilar(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/execution"
	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/tui"
)

func TestEffectiveTUIEventTimeoutDisabledByDefault(t *testing.T) {
	if got := effectiveTUIEventTimeout(2 * time.Minute); got != 0 {
		t.Fatalf("effectiveTUIEventTimeout = %s, want disabled", got)
	}
}

func TestResolveResumeSessionIDRequiresExplicitID(t *testing.T) {
	got, err := resolveResumeSessionID("sess_123")
	if err != nil {
		t.Fatalf("resolveResumeSessionID explicit id: %v", err)
	}
	if got != "sess_123" {
		t.Fatalf("resolveResumeSessionID = %q, want explicit id", got)
	}
	if _, err := resolveResumeSessionID(resumeSelectSentinel); err == nil {
		t.Fatalf("resume selector sentinel should not resolve to a session id")
	}
}

func TestShouldSelectSessionOnStart(t *testing.T) {
	cases := []struct {
		name     string
		resume   string
		sessions []tui.SessionInfo
		want     bool
	}{
		{name: "explicit resume flag", resume: resumeSelectSentinel, want: true},
		{name: "explicit session id", resume: "sess_1", sessions: []tui.SessionInfo{{ID: "sess_2"}}, want: false},
		{name: "plain start with sessions", sessions: []tui.SessionInfo{{ID: "sess_1"}}, want: true},
		{name: "plain start without sessions", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := shouldSelectSessionOnStart(context.Background(), tc.resume, staticSessionLister{sessions: tc.sessions})
			if err != nil {
				t.Fatalf("shouldSelectSessionOnStart: %v", err)
			}
			if got != tc.want {
				t.Fatalf("shouldSelectSessionOnStart = %v, want %v", got, tc.want)
			}
		})
	}
}

type staticSessionLister struct {
	sessions []tui.SessionInfo
}

func (l staticSessionLister) ListSessions(context.Context) ([]tui.SessionInfo, error) {
	return l.sessions, nil
}

func TestTUIRunnerUsesProviderAndModelFlags(t *testing.T) {
	temp := t.TempDir()
	t.Chdir(temp)
	cfg := &config.Config{
		DefaultProvider: "primary",
		DefaultModel:    "primary/model",
		ExecutionMode:   config.ModeWork,
		Providers: map[string]config.ProviderConfig{
			"primary": {Type: "fake"},
			"manual":  {Type: "fake"},
		},
	}
	cmd := newRootCmd()
	cmd.SetContext(context.Background())

	runner, err := newTUIAgentRunner(cmd, cfg, tui.NewPermissionBridge(), "manual", "manual/model")
	if err != nil {
		t.Fatalf("newTUIAgentRunner: %v", err)
	}
	defer runner.Close()

	if runner.providerName != "manual" {
		t.Fatalf("providerName = %q, want manual", runner.providerName)
	}
	if runner.model != "manual/model" {
		t.Fatalf("model = %q, want manual/model", runner.model)
	}
}

func TestTUIRunnerSetProviderSwitchesProviderAndModel(t *testing.T) {
	temp := t.TempDir()
	t.Chdir(temp)
	cfg := &config.Config{
		DefaultProvider: "primary",
		DefaultModel:    "primary/model",
		ExecutionMode:   config.ModeWork,
		Providers: map[string]config.ProviderConfig{
			"primary": {Type: "fake"},
			"manual":  {Type: "fake"},
		},
	}
	cmd := newRootCmd()
	cmd.SetContext(context.Background())
	runner, err := newTUIAgentRunner(cmd, cfg, tui.NewPermissionBridge(), "primary", "primary/model")
	if err != nil {
		t.Fatalf("newTUIAgentRunner: %v", err)
	}
	defer runner.Close()

	state, err := runner.SetProvider("manual", "manual/model")
	if err != nil {
		t.Fatalf("SetProvider: %v", err)
	}
	if runner.providerName != "manual" || runner.model != "manual/model" {
		t.Fatalf("runner provider/model = %q/%q, want manual/manual/model", runner.providerName, runner.model)
	}
	if state.Provider != "manual" || state.Model != "manual/model" {
		t.Fatalf("state = %#v, want manual/manual-model", state)
	}
	if !modelInList(state.Providers, "primary") || !modelInList(state.Providers, "manual") {
		t.Fatalf("providers = %#v, want both configured providers", state.Providers)
	}
}

func TestTUIRunnerNewSessionCreatesBlankSession(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  fake:
    type: fake
`)
	t.Chdir(temp)

	cmd := newRootCmd()
	cmd.SetContext(context.Background())
	runner := &tuiAgentRunner{cmd: cmd, model: "fake/test"}
	defer runner.Close()

	state, err := runner.NewSession(context.Background())
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if state.ID == "" || state.Turn != 0 || len(state.Messages) != 0 {
		t.Fatalf("state = %#v, want empty new session", state)
	}
	sessions := readOnlySessions(t, temp)
	if len(sessions) != 1 || sessions[0].ID != state.ID || sessions[0].Title != "" {
		t.Fatalf("sessions = %#v, want one blank-title new session", sessions)
	}

	if err := finishChatSession(cmd, runner.state, "first prompt", "fake/test"); err != nil {
		t.Fatalf("finishChatSession: %v", err)
	}
	sessions = readOnlySessions(t, temp)
	if len(sessions) != 1 || sessions[0].Title != "first prompt" {
		t.Fatalf("sessions after first prompt = %#v, want title from first prompt", sessions)
	}
}

func TestTUIRunnerModeSwitchPersistsAndRestores(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  fake:
    type: fake
`)
	t.Chdir(temp)

	cmd := newRootCmd()
	cmd.SetContext(context.Background())
	runner := &tuiAgentRunner{cmd: cmd, model: "fake/test", mode: execution.ModeWork}
	defer runner.Close()

	state, err := runner.NewSession(context.Background())
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := runner.SetMode("plan"); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	events := readOnlySessionEvents(t, temp)
	assertEventTypes(t, events, []rollout.Type{rollout.TypeModeSwitch})
	mode, ok, err := rollout.ModeFromEvent(events[0])
	if err != nil {
		t.Fatalf("ModeFromEvent: %v", err)
	}
	if !ok || mode != "plan" {
		t.Fatalf("persisted mode = %q ok=%v, want plan true", mode, ok)
	}

	runner.mode = execution.ModeWork
	if _, err := runner.SwitchSession(context.Background(), state.ID); err != nil {
		t.Fatalf("SwitchSession: %v", err)
	}
	if got := runner.currentMode(); got != execution.ModePlan {
		t.Fatalf("restored mode = %q, want plan", got)
	}
}

func TestTUIRunnerRunShellExecutesBashToolLocally(t *testing.T) {
	temp := t.TempDir()
	t.Chdir(temp)
	runtime, err := newToolRuntime(context.Background(), &config.Config{})
	if err != nil {
		t.Fatalf("newToolRuntime: %v", err)
	}
	defer runtime.Close()
	runner := &tuiAgentRunner{tools: runtime}
	events := make(chan tui.Event, 8)

	if err := runner.RunShell(context.Background(), "printf hello", events); err != nil {
		t.Fatalf("RunShell: %v", err)
	}
	var got []tui.Event
	for len(events) > 0 {
		got = append(got, <-events)
	}
	if len(got) != 2 || got[0].Type != tui.EventShellOutput || got[1].Type != tui.EventDone {
		t.Fatalf("events = %#v, want shell output and done", got)
	}
	for _, event := range got {
		if event.Type == tui.EventActivity {
			t.Fatalf("RunShell emitted tool-like activity event: %#v", event)
		}
	}
	if got[0].Content != "hello" {
		t.Fatalf("shell output = %q, want direct stdout", got[0].Content)
	}
	if strings.Contains(got[0].Content, "exit_code") || strings.Contains(got[0].Content, "duration_ms") {
		t.Fatalf("shell output leaked tool metadata: %q", got[0].Content)
	}
}

func TestFormatShellOutputReportsFailureWithoutToolMetadata(t *testing.T) {
	content := strings.Join([]string{
		"exit_code=7",
		"duration_ms=1",
		"--- stdout ---",
		"",
		"--- stderr ---",
		"bad",
	}, "\n")
	got := formatShellOutput(content, true)
	if got != "bad\nexit code: 7" {
		t.Fatalf("formatShellOutput = %q, want stderr and exit code", got)
	}
	if strings.Contains(got, "duration_ms") || strings.Contains(got, "exit_code=") {
		t.Fatalf("formatShellOutput leaked tool metadata: %q", got)
	}
}

func TestListWorkspaceFilesFiltersAndExcludesHeavyDirs(t *testing.T) {
	root := t.TempDir()
	for path, content := range map[string]string{
		"internal/tui/model.go":        "package tui\n",
		"docs/my note.md":              "note\n",
		".git/config":                  "ignored\n",
		"node_modules/pkg/index.js":    "ignored\n",
		".references/project/file.txt": "ignored\n",
	} {
		abs := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	got, err := listWorkspaceFiles(context.Background(), root, "model", 50)
	if err != nil {
		t.Fatalf("listWorkspaceFiles: %v", err)
	}
	if len(got) != 1 || got[0] != "internal/tui/model.go" {
		t.Fatalf("model matches = %#v, want model.go only", got)
	}
	got, err = listWorkspaceFiles(context.Background(), root, "", 50)
	if err != nil {
		t.Fatalf("listWorkspaceFiles empty query: %v", err)
	}
	for _, path := range got {
		if strings.HasPrefix(path, ".git/") || strings.HasPrefix(path, "node_modules/") || strings.HasPrefix(path, ".references/") {
			t.Fatalf("excluded path surfaced: %#v", got)
		}
	}
}

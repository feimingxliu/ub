package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/feimingxliu/ub/internal/agent"
	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/execution"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/store"
	"github.com/feimingxliu/ub/internal/tool"
	"github.com/feimingxliu/ub/internal/tui"
)

func TestEffectiveTUIEventTimeoutDisabledByDefault(t *testing.T) {
	if got := effectiveTUIEventTimeout(2 * time.Minute); got != 0 {
		t.Fatalf("effectiveTUIEventTimeout = %s, want disabled", got)
	}
}

func TestConvertAgentEventToolPartialOutput(t *testing.T) {
	got := convertAgentEvent(agent.Event{
		Type:      agent.EventToolPartialOutput,
		ToolUseID: "call_1",
		ToolName:  "bash",
		Status:    "stdout",
		Summary:   "cmd=go test ./...",
		Content:   "ok\n",
	})
	if got.Type != tui.EventToolPartialOutput ||
		got.ToolUseID != "call_1" ||
		got.ToolName != "bash" ||
		got.Status != "stdout" ||
		got.Summary != "cmd=go test ./..." ||
		got.Content != "ok\n" ||
		got.IsError {
		t.Fatalf("converted partial event = %#v", got)
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
		name   string
		resume string
		want   bool
	}{
		{name: "explicit resume flag opens picker", resume: resumeSelectSentinel, want: true},
		{name: "explicit session id skips picker", resume: "sess_1", want: false},
		{name: "plain start skips picker even with history", resume: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldSelectSessionOnStart(tc.resume); got != tc.want {
				t.Fatalf("shouldSelectSessionOnStart(%q) = %v, want %v", tc.resume, got, tc.want)
			}
		})
	}
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

func TestTUIRunnerSetProviderPersistsActiveSessionProvider(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  primary:
    type: fake
  manual:
    type: fake
`)
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

	state, err := runner.NewSession(context.Background())
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if _, err := runner.SetProvider("manual", "manual/model"); err != nil {
		t.Fatalf("SetProvider: %v", err)
	}
	sessions := readOnlySessions(t, temp)
	if len(sessions) != 1 || sessions[0].ID != state.ID {
		t.Fatalf("sessions = %#v, want session %s", sessions, state.ID)
	}
	if sessions[0].Provider != "manual" || sessions[0].Model != "manual/model" {
		t.Fatalf("persisted provider/model = %q/%q, want manual/manual/model", sessions[0].Provider, sessions[0].Model)
	}
}

func TestTUIRunnerSwitchSessionRestoresProviderAndModel(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  primary:
    type: fake
  manual:
    type: fake
`)
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
	first, err := newTUIAgentRunner(cmd, cfg, tui.NewPermissionBridge(), "manual", "manual/model")
	if err != nil {
		t.Fatalf("newTUIAgentRunner first: %v", err)
	}
	state, err := first.NewSession(context.Background())
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	second, err := newTUIAgentRunner(cmd, cfg, tui.NewPermissionBridge(), "primary", "primary/model")
	if err != nil {
		t.Fatalf("newTUIAgentRunner second: %v", err)
	}
	defer second.Close()
	restored, err := second.SwitchSession(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("SwitchSession: %v", err)
	}
	if second.providerName != "manual" || second.model != "manual/model" {
		t.Fatalf("restored provider/model = %q/%q, want manual/manual/model", second.providerName, second.model)
	}
	if restored.Provider != "manual" || restored.Model != "manual/model" {
		t.Fatalf("restored state = %#v, want manual/manual-model", restored)
	}
}

func TestTUIRunnerSetProviderKeepsCurrentModelWhenAvailable(t *testing.T) {
	temp := t.TempDir()
	t.Chdir(temp)
	cfg := &config.Config{
		DefaultProvider: "primary",
		DefaultModel:    "primary/model",
		ExecutionMode:   config.ModeWork,
		Providers: map[string]config.ProviderConfig{
			"primary": {Type: "fake"},
			"manual": {
				Type: "fake",
				Models: map[string]config.ModelConfig{
					"shared/model": {},
				},
			},
		},
	}
	cmd := newRootCmd()
	cmd.SetContext(context.Background())
	runner, err := newTUIAgentRunner(cmd, cfg, tui.NewPermissionBridge(), "primary", "shared/model")
	if err != nil {
		t.Fatalf("newTUIAgentRunner: %v", err)
	}
	defer runner.Close()

	state, err := runner.SetProvider("manual", "")
	if err != nil {
		t.Fatalf("SetProvider: %v", err)
	}
	if state.Model != "shared/model" || runner.model != "shared/model" {
		t.Fatalf("model after provider switch = state %q runner %q, want shared/model", state.Model, runner.model)
	}
}

func TestTUIAgentRunnerInitMergesConfiguredAndDiscoveredModels(t *testing.T) {
	requested := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = true
		if r.URL.Path != "/models" {
			t.Fatalf("path = %q, want /models", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"remote/model"},{"id":"configured/model"}]}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		DefaultProvider: "primary",
		ExecutionMode:   config.ModeWork,
		Providers: map[string]config.ProviderConfig{
			"primary": {
				Type:    "openai-compat",
				BaseURL: server.URL,
				Models: map[string]config.ModelConfig{
					"configured/model": {},
				},
			},
		},
	}
	cmd := newRootCmd()
	cmd.SetContext(context.Background())

	runner, err := newTUIAgentRunner(cmd, cfg, tui.NewPermissionBridge(), "", "")
	if err != nil {
		t.Fatalf("newTUIAgentRunner: %v", err)
	}
	defer runner.Close()

	if !requested {
		t.Fatal("provider model list was not requested during initialization")
	}
	got := strings.Join(runner.Models(), "\n")
	want := strings.Join([]string{"configured/model", "remote/model"}, "\n")
	if got != want {
		t.Fatalf("initial models = %s, want %s", got, want)
	}
}

func TestTUIRunnerSetModelPreservesDiscoveredModelsAfterConfiguredSwitch(t *testing.T) {
	cfg := config.ProviderConfig{
		Type:    "openai",
		BaseURL: "https://example.test/v1",
		Models: map[string]config.ModelConfig{
			"configured/model":  {},
			"configured/model2": {},
		},
	}
	cmd := newRootCmd()
	cmd.SetContext(context.Background())
	runner := &tuiAgentRunner{
		cmd:          cmd,
		providerName: "primary",
		providerCfg:  cfg,
		model:        "configured/model",
		models:       []string{"configured/model", "configured/model2"},
		providerChecks: map[string]providerCheck{
			providerCheckKey("primary", cfg): {
				Status: "ok",
				Models: []string{"configured/model", "configured/model2", "remote/model"},
			},
		},
	}

	if err := runner.SetModel("configured/model2"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	if runner.model != "configured/model2" {
		t.Fatalf("model = %q, want configured/model2", runner.model)
	}
	want := []string{"configured/model2", "configured/model", "remote/model"}
	got := strings.Join(runner.Models(), "\n")
	if strings.Join(want, "\n") != got {
		t.Fatalf("models = %q, want %q", got, strings.Join(want, ","))
	}
}

func TestTUIRunnerRefreshModelsMergesConfiguredAndProviderModels(t *testing.T) {
	cfg := config.ProviderConfig{
		Type:    "openai-compat",
		BaseURL: "https://example.test/v1",
		Models: map[string]config.ModelConfig{
			"configured/model": {},
		},
	}
	runner := &tuiAgentRunner{
		providerName: "primary",
		providerCfg:  cfg,
		model:        "current/model",
		providerChecks: map[string]providerCheck{
			providerCheckKey("primary", cfg): {
				Status: "ok",
				Models: []string{"remote/model", "configured/model"},
			},
		},
	}

	models, err := runner.RefreshModels(context.Background())
	if err != nil {
		t.Fatalf("RefreshModels: %v", err)
	}
	want := []string{"current/model", "configured/model", "remote/model"}
	if strings.Join(models, "\n") != strings.Join(want, "\n") {
		t.Fatalf("models = %#v, want %#v", models, want)
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

	if err := finishChatSession(cmd, runner.state, "first prompt", "fake", "fake/test"); err != nil {
		t.Fatalf("finishChatSession: %v", err)
	}
	sessions = readOnlySessions(t, temp)
	if len(sessions) != 1 || sessions[0].Title != "first prompt" {
		t.Fatalf("sessions after first prompt = %#v, want title from first prompt", sessions)
	}
}

func TestTUIRunnerSearchSessionsWithoutActiveState(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(temp, "data"))
	t.Chdir(temp)

	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	now := time.UnixMilli(1_700_000_000_000).UTC()
	if err := st.CreateSession(context.Background(), store.Session{
		ID:        "searchable",
		Workspace: mustCanonicalTestWorkspace(t, temp),
		Title:     "Searchable Session",
		Model:     "fake/model",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	ro, err := rollout.New(st)
	if err != nil {
		t.Fatalf("rollout.New: %v", err)
	}
	event, err := rollout.UserMessage("searchable", 1, message.Text(message.RoleUser, "find this needle"))
	if err != nil {
		t.Fatalf("UserMessage: %v", err)
	}
	if err := ro.Append(context.Background(), event); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	runner := &tuiAgentRunner{}
	got, err := runner.SearchSessions(context.Background(), "needle", 50)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	for _, want := range []string{"searchable", "turn 1", "find this needle"} {
		if !strings.Contains(got, want) {
			t.Fatalf("SearchSessions output missing %q:\n%s", want, got)
		}
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
	restored, err := runner.SwitchSession(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("SwitchSession: %v", err)
	}
	if got := runner.currentMode(); got != execution.ModePlan {
		t.Fatalf("restored mode = %q, want plan", got)
	}
	if restored.Mode != "plan" {
		t.Fatalf("restored state mode = %q, want plan", restored.Mode)
	}
}

func TestMessagesForTUIRestoresToolActivity(t *testing.T) {
	messages := []message.Message{
		message.Text(message.RoleUser, "inspect"),
		message.New(message.RoleAssistant, message.ToolUseBlock("call_read", "read", json.RawMessage(`{"path":"README.md"}`))),
		message.New(message.RoleTool, message.ToolResultBlock("call_read", "file content", false)),
		message.Text(message.RoleAssistant, "done"),
	}

	got := messagesForTUI(messages)
	if len(got) != 4 {
		t.Fatalf("messagesForTUI len = %d, want 4: %#v", len(got), got)
	}
	if got[1].ActivityKind != "tool" || got[1].Status != "queued" || got[1].ToolName != "read" || got[1].Summary != "path=README.md" {
		t.Fatalf("queued tool activity = %#v", got[1])
	}
	if got[2].ActivityKind != "tool" || got[2].Status != "done" || got[2].Content != "file content" {
		t.Fatalf("done tool activity = %#v", got[2])
	}
}

func TestMessagesForTUIRestoresShellActivityWithoutMetadataTags(t *testing.T) {
	shellOutput := "<shell_metadata>\nexit_code=0\nduration_ms=5\n</shell_metadata>\n--- stdout ---\nok\n--- stderr ---\n"
	messages := []message.Message{
		message.New(message.RoleAssistant, message.ToolUseBlock("call_bash", "bash", json.RawMessage(`{"command":"echo ok"}`))),
		message.New(message.RoleTool, message.ToolResultBlock("call_bash", shellOutput, false)),
	}

	got := messagesForTUI(messages)
	if len(got) != 2 {
		t.Fatalf("messagesForTUI len = %d, want 2: %#v", len(got), got)
	}
	if strings.Contains(got[1].Content, "<shell_metadata>") || !strings.Contains(got[1].Content, "ok") {
		t.Fatalf("shell detail = %q, want formatted output without metadata tags", got[1].Content)
	}
}

func TestMessagesForTUIFromRolloutRestoresThinkingActivity(t *testing.T) {
	userEvent, err := rollout.UserMessage("sess_1", 1, message.Text(message.RoleUser, "inspect"))
	if err != nil {
		t.Fatalf("UserMessage: %v", err)
	}
	thinkingEvent, err := rollout.Activity("sess_1", 1, rollout.ActivityPayload{
		ActivityKind: "thinking",
		Summary:      "checking files",
		Content:      "checking files in repo",
	})
	if err != nil {
		t.Fatalf("Activity: %v", err)
	}
	assistantEvent, err := rollout.AssistantMessage("sess_1", 1, message.Text(message.RoleAssistant, "done"))
	if err != nil {
		t.Fatalf("AssistantMessage: %v", err)
	}

	got, err := messagesForTUIFromRollout(context.Background(), staticRolloutReader{
		events: []rollout.Event{userEvent, thinkingEvent, assistantEvent},
	}, "sess_1")
	if err != nil {
		t.Fatalf("messagesForTUIFromRollout: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("messages len = %d, want 3: %#v", len(got), got)
	}
	if got[1].ActivityKind != "thinking" || got[1].Summary != "checking files" || got[1].Content != "checking files in repo" {
		t.Fatalf("thinking activity = %#v", got[1])
	}
}

func TestMessagesForTUIFromRolloutUsesToolResultPayloadDetail(t *testing.T) {
	assistantEvent, err := rollout.AssistantMessage("sess_1", 1,
		message.New(message.RoleAssistant, message.ToolUseBlock("call_plan", "plan_write", json.RawMessage(`{"title":"Plan","steps":["inspect"]}`))))
	if err != nil {
		t.Fatalf("AssistantMessage: %v", err)
	}
	toolResultEvent, err := rollout.ToolResult("sess_1", 1, "call_plan", "plan_write", tool.Result{
		Content: "plan_id=plan-1\npath=/home/user/.local/state/ub/plans/abc123/plan-1.md\n\n# Plan\n\n- [ ] inspect",
		Files:   []tool.FileChange{{Path: "/home/user/.local/state/ub/plans/abc123/plan-1.md", Kind: tool.KindCreate}},
	})
	if err != nil {
		t.Fatalf("ToolResult: %v", err)
	}

	got, err := messagesForTUIFromRollout(context.Background(), staticRolloutReader{
		events: []rollout.Event{assistantEvent, toolResultEvent},
	}, "sess_1")
	if err != nil {
		t.Fatalf("messagesForTUIFromRollout: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("messages len = %d, want 2: %#v", len(got), got)
	}
	if got[1].Summary != "create /home/user/.local/state/ub/plans/abc123/plan-1.md" || !strings.Contains(got[1].Content, "# Plan") {
		t.Fatalf("tool result activity = %#v", got[1])
	}
}

func TestMessagesForTUIFromRolloutTagsTurnNumber(t *testing.T) {
	events := []rollout.Event{}
	for turn := 1; turn <= 2; turn++ {
		userEvt, err := rollout.UserMessage("sess_1", turn, message.Text(message.RoleUser, "prompt"))
		if err != nil {
			t.Fatalf("UserMessage: %v", err)
		}
		toolUseEvt, err := rollout.AssistantMessage("sess_1", turn,
			message.New(message.RoleAssistant, message.ToolUseBlock(fmt.Sprintf("call_%d", turn), "read", json.RawMessage(`{"path":"x"}`))))
		if err != nil {
			t.Fatalf("AssistantMessage: %v", err)
		}
		toolResultEvt, err := rollout.ToolResult("sess_1", turn, fmt.Sprintf("call_%d", turn), "read", tool.Result{Content: "ok"})
		if err != nil {
			t.Fatalf("ToolResult: %v", err)
		}
		events = append(events, userEvt, toolUseEvt, toolResultEvt)
	}

	got, err := messagesForTUIFromRollout(context.Background(), staticRolloutReader{events: events}, "sess_1")
	if err != nil {
		t.Fatalf("messagesForTUIFromRollout: %v", err)
	}

	// Each turn produces: user text, tool_use activity, tool_result activity.
	// Tools from turn 1 and turn 2 must carry distinct Turn values so the TUI
	// can namespace activity groups instead of collapsing them into one block.
	turnByCall := map[string]int{}
	for _, m := range got {
		if m.ActivityKind == "tool" && m.ToolUseID != "" {
			turnByCall[m.ToolUseID] = m.Turn
		}
	}
	if turnByCall["call_1"] != 1 || turnByCall["call_2"] != 2 {
		t.Fatalf("tool Turn assignments wrong: %#v", turnByCall)
	}
}

func TestTUIRunnerRunShellExecutesBashToolLocally(t *testing.T) {
	temp := t.TempDir()
	t.Chdir(temp)
	toolRt, err := newToolRuntime(context.Background(), &config.Config{})
	if err != nil {
		t.Fatalf("newToolRuntime: %v", err)
	}
	defer toolRt.Close()
	runner := &tuiAgentRunner{tools: toolRt}
	events := make(chan tui.Event, 8)

	shellCmd := "echo hello"
	if err := runner.RunShell(context.Background(), shellCmd, events); err != nil {
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
	if strings.TrimSpace(got[0].Content) != "hello" {
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

type staticRolloutReader struct {
	events []rollout.Event
}

func (r staticRolloutReader) ForEach(_ context.Context, sessionID string, fn func(rollout.Event) error) error {
	for _, event := range r.events {
		if event.SessionID != sessionID {
			continue
		}
		if err := fn(event); err != nil {
			return err
		}
	}
	return nil
}

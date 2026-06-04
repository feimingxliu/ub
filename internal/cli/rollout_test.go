package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/store"
	"github.com/feimingxliu/ub/internal/tool"
)

func TestRolloutShowPrettyPrintsFilteredTurns(t *testing.T) {
	sessionID := createRolloutShowFixture(t)

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"rollout", "show", sessionID, "--turns", "2..3"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("rollout show: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"session " + sessionID,
		"title: Rollout Fixture",
		"turn 2",
		"tool_use: read id=call_1",
		`"path": "README.md"`,
		"tool: read id=call_1 status=ok",
		"output: file text",
		"file: main.go modify",
		"turn 3",
		"summary: compressed=4 kept=2 estimated_tokens=120",
		"text: Earlier summary.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rollout show output missing %q:\n%s", want, got)
		}
	}
	for _, notWant := range []string{"hello", "answer", "boom"} {
		if strings.Contains(got, notWant) {
			t.Fatalf("rollout show --turns leaked %q:\n%s", notWant, got)
		}
	}
}

func TestRolloutShowJSONL(t *testing.T) {
	sessionID := createRolloutShowFixture(t)

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"rollout", "show", sessionID, "--json", "--turns", "1"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("rollout show --json: %v", err)
	}
	var events []rollout.Event
	scanner := bufio.NewScanner(strings.NewReader(out.String()))
	for scanner.Scan() {
		var event rollout.Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("decode JSONL line %q: %v", scanner.Text(), err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("events len = %d, want 3; output:\n%s", len(events), out.String())
	}
	for _, event := range events {
		if event.Turn != 1 || event.SessionID != sessionID {
			t.Fatalf("event = %#v, want session %s turn 1", event, sessionID)
		}
	}
	if events[0].Type != rollout.TypeUserMessage || events[1].Type != rollout.TypeAssistantMessage || events[2].Type != rollout.TypeUsage {
		t.Fatalf("event types = %s, %s, %s", events[0].Type, events[1].Type, events[2].Type)
	}
}

func TestRolloutShowMissingSession(t *testing.T) {
	temp := t.TempDir()
	workspace := filepath.Join(temp, "repo")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", filepath.Join(temp, "data"))
	t.Chdir(workspace)

	cmd := newRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"rollout", "show", "missing"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected missing session error")
	}
	if !strings.Contains(err.Error(), "session") || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing session error = %v", err)
	}
}

func TestParseTurnFilter(t *testing.T) {
	cases := []struct {
		raw        string
		wantTurn1  bool
		wantTurn5  bool
		wantTurn10 bool
	}{
		{raw: "", wantTurn1: true, wantTurn5: true, wantTurn10: true},
		{raw: "5", wantTurn1: false, wantTurn5: true, wantTurn10: false},
		{raw: "5..10", wantTurn1: false, wantTurn5: true, wantTurn10: true},
		{raw: "..5", wantTurn1: true, wantTurn5: true, wantTurn10: false},
		{raw: "5..", wantTurn1: false, wantTurn5: true, wantTurn10: true},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			filter, err := parseTurnFilter(tc.raw)
			if err != nil {
				t.Fatalf("parseTurnFilter(%q): %v", tc.raw, err)
			}
			if got := filter.include(1); got != tc.wantTurn1 {
				t.Fatalf("include(1) = %v, want %v", got, tc.wantTurn1)
			}
			if got := filter.include(5); got != tc.wantTurn5 {
				t.Fatalf("include(5) = %v, want %v", got, tc.wantTurn5)
			}
			if got := filter.include(10); got != tc.wantTurn10 {
				t.Fatalf("include(10) = %v, want %v", got, tc.wantTurn10)
			}
		})
	}

	for _, raw := range []string{"0", "10..5", "..", "a..b", "1..2..3"} {
		t.Run("invalid "+raw, func(t *testing.T) {
			if _, err := parseTurnFilter(raw); err == nil {
				t.Fatalf("parseTurnFilter(%q) returned nil error", raw)
			}
		})
	}
}

func createRolloutShowFixture(t *testing.T) string {
	t.Helper()
	temp := t.TempDir()
	workspace := filepath.Join(temp, "repo")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", filepath.Join(temp, "data"))
	t.Chdir(workspace)

	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	now := time.UnixMilli(1_700_000_000_000).UTC()
	sessionID := "sess_rollout_show"
	if err := st.CreateSession(context.Background(), store.Session{
		ID:        sessionID,
		Workspace: workspace,
		Title:     "Rollout Fixture",
		Model:     "fake/model",
		CreatedAt: now,
		UpdatedAt: now.Add(5 * time.Minute),
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	ro, err := rollout.New(st)
	if err != nil {
		t.Fatalf("rollout.New: %v", err)
	}
	appendRolloutEventAt(t, ro, now, mustRolloutEvent(rollout.UserMessage(sessionID, 1, message.Text(message.RoleUser, "hello"))))
	appendRolloutEventAt(t, ro, now.Add(time.Second), mustRolloutEvent(rollout.AssistantMessage(sessionID, 1, message.Text(message.RoleAssistant, "answer"))))
	appendRolloutEventAt(t, ro, now.Add(2*time.Second), mustRolloutEvent(rollout.Usage(sessionID, 1, 3, 4)))
	appendRolloutEventAt(t, ro, now.Add(2500*time.Millisecond), mustRolloutEvent(rollout.AssistantMessage(sessionID, 2,
		message.New(message.RoleAssistant, message.ToolUseBlock("call_1", "read", json.RawMessage(`{"path":"README.md"}`))))))
	appendRolloutEventAt(t, ro, now.Add(3*time.Second), mustRolloutEvent(rollout.ToolResult(sessionID, 2, "call_1", "read", tool.Result{
		Content: "file text",
		Files: []tool.FileChange{{
			Path:        "main.go",
			Kind:        tool.KindModify,
			UnifiedDiff: "@@\n-old\n+new\n",
		}},
	})))
	appendRolloutEventAt(t, ro, now.Add(4*time.Second), mustRolloutEvent(rollout.Summary(sessionID, 3, "Earlier summary.", 4, 2, 120)))
	appendRolloutEventAt(t, ro, now.Add(5*time.Second), mustRolloutEvent(rollout.Error(sessionID, 4, errors.New("boom"))))
	return sessionID
}

func mustRolloutEvent(event rollout.Event, err error) rollout.Event {
	if err != nil {
		panic(err)
	}
	return event
}

func appendRolloutEventAt(t *testing.T, ro *rollout.SQLite, at time.Time, event rollout.Event) {
	t.Helper()
	event.Time = at
	if err := ro.Append(context.Background(), event); err != nil {
		t.Fatalf("Append(%s): %v", event.Type, err)
	}
}

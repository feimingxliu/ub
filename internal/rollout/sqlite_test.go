package rollout

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/store"
	"github.com/feimingxliu/ub/internal/tool"
)

func TestAppendValidatesRequiredFields(t *testing.T) {
	ro := openRollout(t)
	err := ro.Append(context.Background(), Event{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if got := err.Error(); got == "" {
		t.Fatal("validation error should be readable")
	}
}

func TestAppendAndReadHundredEventsInOrder(t *testing.T) {
	ctx := context.Background()
	st, ro := openStoreRollout(t)
	sessionID := createSession(t, st, "order")

	base := time.Unix(100, 0).UTC()
	for i := 99; i >= 0; i-- {
		payload, err := MarshalPayload(map[string]int{"i": i})
		if err != nil {
			t.Fatal(err)
		}
		event := Event{
			ID:        "evt_" + strconv.Itoa(i),
			SessionID: sessionID,
			Turn:      i/10 + 1,
			Time:      base.Add(time.Duration(i) * time.Millisecond),
			Type:      TypeUserMessage,
			Payload:   payload,
		}
		if err := ro.Append(ctx, event); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	var got []Event
	if err := ro.ForEach(ctx, sessionID, func(event Event) error {
		got = append(got, event)
		return nil
	}); err != nil {
		t.Fatalf("ForEach: %v", err)
	}
	if len(got) != 100 {
		t.Fatalf("events len = %d, want 100", len(got))
	}
	for i, event := range got {
		var payload map[string]int
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("payload %d: %v", i, err)
		}
		if payload["i"] != i {
			t.Fatalf("event %d payload = %#v", i, payload)
		}
	}
}

func TestReaderFiltersSession(t *testing.T) {
	ctx := context.Background()
	st, ro := openStoreRollout(t)
	first := createSession(t, st, "first")
	second := createSession(t, st, "second")
	appendMessage(t, ro, first, "first")
	appendMessage(t, ro, second, "second")

	var got []Event
	if err := ro.ForEach(ctx, first, func(event Event) error {
		got = append(got, event)
		return nil
	}); err != nil {
		t.Fatalf("ForEach: %v", err)
	}
	if len(got) != 1 || got[0].SessionID != first {
		t.Fatalf("filtered events = %#v", got)
	}
}

func TestToolResultEventAndMessageFromEvent(t *testing.T) {
	event, err := ToolResult("sess_tool", 2, "call_1", "read", tool.Result{
		Content: "file content",
		IsError: true,
		Files: []tool.FileChange{{
			Path: "main.go",
			Kind: tool.KindModify,
		}},
	})
	if err != nil {
		t.Fatalf("ToolResult: %v", err)
	}
	if event.Type != TypeToolResult {
		t.Fatalf("event type = %q, want %q", event.Type, TypeToolResult)
	}
	var payload ToolResultPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload.ToolUseID != "call_1" || payload.ToolName != "read" || payload.Output != "file content" || !payload.IsError {
		t.Fatalf("payload = %#v", payload)
	}
	msg, ok, err := MessageFromEvent(event)
	if err != nil {
		t.Fatalf("MessageFromEvent: %v", err)
	}
	if !ok || msg.Role != message.RoleUser || len(msg.Content) != 1 {
		t.Fatalf("message = %#v, ok=%v", msg, ok)
	}
	block := msg.Content[0]
	if block.Type != message.BlockToolResult || block.ToolUseID != "call_1" || block.Output != "file content" || !block.IsError {
		t.Fatalf("block = %#v", block)
	}
}

func TestAppendVisibleAfterReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ub.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	sessionID := createSession(t, st, "durable")
	ro, err := New(st)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	appendMessage(t, ro, sessionID, "durable")
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	st, err = store.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st.Close()
	ro, err = New(st)
	if err != nil {
		t.Fatalf("New reopened: %v", err)
	}
	var count int
	if err := ro.ForEach(ctx, sessionID, func(event Event) error {
		count++
		return nil
	}); err != nil {
		t.Fatalf("ForEach reopened: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}

func TestAppendSurvivesProcessExit(t *testing.T) {
	if os.Getenv("UB_ROLLOUT_CRASH_CHILD") == "1" {
		crashChild(t)
		return
	}
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ub.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	sessionID := createSession(t, st, "crash")
	if err := st.Close(); err != nil {
		t.Fatalf("close parent setup: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestAppendSurvivesProcessExit$")
	cmd.Env = append(os.Environ(),
		"UB_ROLLOUT_CRASH_CHILD=1",
		"UB_ROLLOUT_CRASH_DB="+path,
		"UB_ROLLOUT_CRASH_SESSION="+sessionID,
	)
	err = cmd.Run()
	if err == nil {
		t.Fatal("child should exit non-zero")
	}

	st, err = store.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st.Close()
	ro, err := New(st)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var count int
	if err := ro.ForEach(ctx, sessionID, func(event Event) error {
		count++
		return nil
	}); err != nil {
		t.Fatalf("ForEach: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}

func TestForEachCallbackError(t *testing.T) {
	st, ro := openStoreRollout(t)
	sessionID := createSession(t, st, "callback")
	appendMessage(t, ro, sessionID, "callback")
	want := errors.New("stop")
	err := ro.ForEach(context.Background(), sessionID, func(event Event) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("ForEach error = %v, want %v", err, want)
	}
}

func crashChild(t *testing.T) {
	t.Helper()
	st, err := store.Open(os.Getenv("UB_ROLLOUT_CRASH_DB"))
	if err != nil {
		t.Fatalf("child open: %v", err)
	}
	ro, err := New(st)
	if err != nil {
		t.Fatalf("child rollout: %v", err)
	}
	appendMessage(t, ro, os.Getenv("UB_ROLLOUT_CRASH_SESSION"), "crash")
	os.Exit(1)
}

func openRollout(t *testing.T) *SQLite {
	t.Helper()
	_, ro := openStoreRollout(t)
	return ro
}

func openStoreRollout(t *testing.T) (*store.Store, *SQLite) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "ub.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ro, err := New(st)
	if err != nil {
		t.Fatalf("New rollout: %v", err)
	}
	return st, ro
}

func createSession(t *testing.T, st *store.Store, suffix string) string {
	t.Helper()
	id := "sess_" + suffix
	if err := st.CreateSession(context.Background(), store.Session{
		ID:        id,
		Workspace: "/workspace",
		Title:     suffix,
		Model:     "fake/model",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return id
}

func appendMessage(t *testing.T, ro *SQLite, sessionID, text string) {
	t.Helper()
	event, err := UserMessage(sessionID, 1, message.Text(message.RoleUser, text))
	if err != nil {
		t.Fatalf("UserMessage: %v", err)
	}
	if err := ro.Append(context.Background(), event); err != nil {
		t.Fatalf("Append: %v", err)
	}
}

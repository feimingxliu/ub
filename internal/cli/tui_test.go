package cli

import (
	"context"
	"testing"
	"time"
)

func TestEffectiveTUIEventTimeoutDisabledByDefault(t *testing.T) {
	if got := effectiveTUIEventTimeout(2 * time.Minute); got != 0 {
		t.Fatalf("effectiveTUIEventTimeout = %s, want disabled", got)
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

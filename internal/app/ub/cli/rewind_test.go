package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/app/ub/tui"
	"github.com/feimingxliu/ub/internal/pkg/core/execution"
	"github.com/feimingxliu/ub/internal/pkg/core/message"
	"github.com/feimingxliu/ub/internal/pkg/tool"
	"github.com/feimingxliu/ub/internal/pkg/workspace/filehistory"
	"github.com/feimingxliu/ub/internal/pkg/workspace/rollout"
)

func TestTUIRunnerListRewindTargetsIncludesCheckpointAffectedFiles(t *testing.T) {
	runner, temp := newRewindTestRunner(t)
	writeFile(t, temp, "main.go", "old\n")
	fh := newRewindTestFileHistory(t, runner)
	if err := fh.MakeSnapshot(context.Background(), 1); err != nil {
		t.Fatalf("MakeSnapshot: %v", err)
	}
	if err := fh.TrackTool(context.Background(), "edit", json.RawMessage(`{"path":"main.go","old":"old","new":"new"}`)); err != nil {
		t.Fatalf("TrackTool: %v", err)
	}
	writeFile(t, temp, "main.go", "new\n")
	appendRunnerEvent(t, runner, mustRolloutEvent(rollout.UserMessage(runner.state.sessionID, 1, message.Text(message.RoleUser, "change file"))))
	appendRunnerEvent(t, runner, mustRolloutEvent(rollout.UserMessage(runner.state.sessionID, 2, message.Text(message.RoleUser, "second"))))

	targets, err := runner.ListRewindTargets(context.Background())
	if err != nil {
		t.Fatalf("ListRewindTargets: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("targets len = %d, want 2: %#v", len(targets), targets)
	}
	if targets[1].Turn != 1 || targets[1].Text != "change file" {
		t.Fatalf("turn 1 target = %#v", targets[1])
	}
	if len(targets[1].AffectedFiles) != 1 ||
		targets[1].AffectedFiles[0].Path != "main.go" ||
		targets[1].AffectedFiles[0].Kind != tool.KindModify {
		t.Fatalf("turn 1 affected files = %#v, want main.go modify", targets[1].AffectedFiles)
	}
}

func TestTUIRunnerRewindConversationOnlyDeletesEvents(t *testing.T) {
	runner, temp := newRewindTestRunner(t)
	appendRunnerEvent(t, runner, mustRolloutEvent(rollout.UserMessage(runner.state.sessionID, 1, message.Text(message.RoleUser, "first"))))
	appendRunnerEvent(t, runner, mustRolloutEvent(rollout.AssistantMessage(runner.state.sessionID, 1, message.Text(message.RoleAssistant, "ok"))))
	appendRunnerEvent(t, runner, mustRolloutEvent(rollout.UserMessage(runner.state.sessionID, 2, message.Text(message.RoleUser, "second"))))
	appendRunnerEvent(t, runner, mustRolloutEvent(rollout.AssistantMessage(runner.state.sessionID, 2, message.Text(message.RoleAssistant, "bad"))))

	state, result, err := runner.Rewind(context.Background(), tui.RewindRequest{Turn: 2})
	if err != nil {
		t.Fatalf("Rewind: %v", err)
	}
	if state.Turn != 1 || result.Target.Text != "second" || result.DeletedEvents != 2 {
		t.Fatalf("state=%#v result=%#v, want turn 1 deleted second turn", state, result)
	}
	if runner.state.nextTurn != 2 {
		t.Fatalf("nextTurn = %d, want 2", runner.state.nextTurn)
	}
	events := readOnlySessionEvents(t, temp)
	if len(events) != 2 || events[0].Turn != 1 || events[1].Turn != 1 {
		t.Fatalf("remaining events = %#v, want only turn 1", events)
	}
}

func TestTUIRunnerRewindCanRestoreCheckpointFileChange(t *testing.T) {
	runner, temp := newRewindTestRunner(t)
	writeFile(t, temp, "main.go", "old\n")
	fh := newRewindTestFileHistory(t, runner)
	if err := fh.MakeSnapshot(context.Background(), 1); err != nil {
		t.Fatalf("MakeSnapshot: %v", err)
	}
	if err := fh.TrackTool(context.Background(), "edit", json.RawMessage(`{"path":"main.go","old":"old","new":"new"}`)); err != nil {
		t.Fatalf("TrackTool: %v", err)
	}
	writeFile(t, temp, "main.go", "new\n")
	appendRunnerEvent(t, runner, mustRolloutEvent(rollout.UserMessage(runner.state.sessionID, 1, message.Text(message.RoleUser, "change file"))))

	state, result, err := runner.Rewind(context.Background(), tui.RewindRequest{Turn: 1, RevertFiles: true})
	if err != nil {
		t.Fatalf("Rewind: %v", err)
	}
	if state.Turn != 0 || result.Target.Text != "change file" {
		t.Fatalf("state=%#v result=%#v, want empty session before turn 1", state, result)
	}
	assertFileContent(t, temp, "main.go", "old\n")
	if len(result.RevertedFiles) != 1 || result.RevertedFiles[0] != "main.go modify" {
		t.Fatalf("reverted files = %#v, want main.go modify", result.RevertedFiles)
	}
}

func TestTUIRunnerRewindRestoresBashDeletedCheckpointFiles(t *testing.T) {
	runner, temp := newRewindTestRunner(t)
	writeFile(t, temp, "docs/shell-completion.md", "completion docs\n")
	writeFile(t, temp, "examples/bad-refactor/.gitignore", "node_modules\n")

	fh := newRewindTestFileHistory(t, runner)
	if err := fh.MakeSnapshot(context.Background(), 1); err != nil {
		t.Fatalf("MakeSnapshot: %v", err)
	}
	if err := fh.TrackTool(context.Background(), "bash", json.RawMessage(`{"command":"rm docs/shell-completion.md examples/bad-refactor/.gitignore"}`)); err != nil {
		t.Fatalf("TrackTool: %v", err)
	}
	removeFile(t, temp, "docs/shell-completion.md")
	removeFile(t, temp, "examples/bad-refactor/.gitignore")

	appendRunnerEvent(t, runner, mustRolloutEvent(rollout.UserMessage(runner.state.sessionID, 1, message.Text(message.RoleUser, "delete files"))))
	appendRunnerEvent(t, runner, mustRolloutEvent(rollout.AssistantMessage(runner.state.sessionID, 1,
		message.New(message.RoleAssistant, message.ToolUseBlock("call_rm", "bash", json.RawMessage(`{"command":"rm docs/shell-completion.md examples/bad-refactor/.gitignore"}`))))))
	appendRunnerEvent(t, runner, mustRolloutEvent(rollout.ToolResult(runner.state.sessionID, 1, "call_rm", "bash", tool.Result{
		Content: "exit_code=0",
	})))

	_, result, err := runner.Rewind(context.Background(), tui.RewindRequest{Turn: 1, RevertFiles: true})
	if err != nil {
		t.Fatalf("Rewind: %v", err)
	}
	assertFileContent(t, temp, "docs/shell-completion.md", "completion docs\n")
	assertFileContent(t, temp, "examples/bad-refactor/.gitignore", "node_modules\n")
	joined := strings.Join(result.RevertedFiles, "\n")
	for _, rel := range []string{"docs/shell-completion.md", "examples/bad-refactor/.gitignore"} {
		if !strings.Contains(joined, rel+" delete") {
			t.Fatalf("reverted files = %#v, want %s delete", result.RevertedFiles, rel)
		}
	}
}

func TestTUIRunnerRewindDeletesFileCreatedAfterCheckpoint(t *testing.T) {
	runner, temp := newRewindTestRunner(t)
	fh := newRewindTestFileHistory(t, runner)
	if err := fh.MakeSnapshot(context.Background(), 1); err != nil {
		t.Fatalf("MakeSnapshot: %v", err)
	}
	if err := fh.TrackTool(context.Background(), "write", json.RawMessage(`{"path":"new.txt","content":"new"}`)); err != nil {
		t.Fatalf("TrackTool: %v", err)
	}
	writeFile(t, temp, "new.txt", "new")
	appendRunnerEvent(t, runner, mustRolloutEvent(rollout.UserMessage(runner.state.sessionID, 1, message.Text(message.RoleUser, "create file"))))

	_, result, err := runner.Rewind(context.Background(), tui.RewindRequest{Turn: 1, RevertFiles: true})
	if err != nil {
		t.Fatalf("Rewind: %v", err)
	}
	if _, err := os.Stat(filepath.Join(temp, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new.txt stat err = %v, want not exist", err)
	}
	if len(result.RevertedFiles) != 1 || result.RevertedFiles[0] != "new.txt create" {
		t.Fatalf("reverted files = %#v, want new.txt create", result.RevertedFiles)
	}
}

func newRewindTestRunner(t *testing.T) (*tuiAgentRunner, string) {
	t.Helper()
	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  fake:
    type: fake
`)
	t.Chdir(temp)
	cmd := newRootCmd()
	cmd.SetContext(context.Background())
	runner := &tuiAgentRunner{
		cmd:   cmd,
		model: "fake/test",
		mode:  execution.ModeWork,
		tools: &toolRuntime{Workspace: temp},
	}
	t.Cleanup(func() { _ = runner.Close() })
	if _, err := runner.NewSession(context.Background()); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	return runner, temp
}

func newRewindTestFileHistory(t *testing.T, runner *tuiAgentRunner) *filehistory.Manager {
	t.Helper()
	fh, err := newFileHistoryManager(context.Background(), runner.tools.Workspace, runner.state.sessionID, runner.state.rollout)
	if err != nil {
		t.Fatalf("newFileHistoryManager: %v", err)
	}
	return fh
}

func appendRunnerEvent(t *testing.T, runner *tuiAgentRunner, event rollout.Event) {
	t.Helper()
	if err := runner.state.rollout.Append(context.Background(), event); err != nil {
		t.Fatalf("Append(%s): %v", event.Type, err)
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func removeFile(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		t.Fatalf("remove %s: %v", rel, err)
	}
}

func assertFileContent(t *testing.T, root, rel, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	if string(got) != want {
		t.Fatalf("%s content = %q, want %q", rel, got, want)
	}
}

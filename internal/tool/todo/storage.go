package todo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/feimingxliu/ub/internal/workspace/paths"
)

const (
	todosDirPerm = 0o755
	todoFilePerm = 0o644
)

var unsafePathPart = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

type item struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"`
	Note    string `json:"note,omitempty"`
}

type list struct {
	Items []item `json:"items"`
}

func statePath(sessionID string) (string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return "", fmt.Errorf("todo: session id is required")
	}
	root, err := paths.StateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "todos", safePathPart(sessionID)+".json"), nil
}

func safePathPart(value string) string {
	value = strings.TrimSpace(value)
	value = unsafePathPart.ReplaceAllString(value, "_")
	value = strings.Trim(value, "._-")
	if value == "" {
		return "session"
	}
	if len(value) > 80 {
		value = value[:80]
		value = strings.Trim(value, "._-")
	}
	if value == "" {
		return "session"
	}
	return value
}

func load(sessionID string) (list, error) {
	path, err := statePath(sessionID)
	if err != nil {
		return list{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return list{}, fmt.Errorf("todo list not found for session %s; call todo_write first", sessionID)
		}
		return list{}, err
	}
	var l list
	if err := json.Unmarshal(raw, &l); err != nil {
		return list{}, fmt.Errorf("decode todo list: %w", err)
	}
	return l, nil
}

func save(sessionID string, l list) error {
	path, err := statePath(sessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), todosDirPerm); err != nil {
		return fmt.Errorf("create todo dir: %w", err)
	}
	raw, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("encode todo list: %w", err)
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".todo-*.tmp")
	if err != nil {
		return fmt.Errorf("create todo tmp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("write todo tmp: %w", err)
	}
	if err := tmp.Chmod(todoFilePerm); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod todo tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close todo tmp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename todo: %w", err)
	}
	tmpName = ""
	return nil
}

func normalizeStatus(status string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "pending":
		return "pending", nil
	case "in_progress", "in-progress", "running", "started":
		return "in_progress", nil
	case "completed", "complete", "done":
		return "completed", nil
	case "skipped", "skip":
		return "skipped", nil
	case "failed", "error":
		return "failed", nil
	default:
		return "", fmt.Errorf("invalid status %q (want pending|in_progress|completed|skipped|failed)", status)
	}
}

func marker(status string) string {
	switch status {
	case "in_progress":
		return ">"
	case "completed":
		return "x"
	case "skipped":
		return "~"
	case "failed":
		return "!"
	default:
		return " "
	}
}

func validate(l list) error {
	if len(l.Items) == 0 {
		return fmt.Errorf("todo list must contain at least one item")
	}
	seen := map[string]struct{}{}
	inProgress := 0
	for i := range l.Items {
		it := &l.Items[i]
		it.ID = strings.TrimSpace(it.ID)
		if it.ID == "" {
			it.ID = fmt.Sprintf("%d", i+1)
		}
		if _, ok := seen[it.ID]; ok {
			return fmt.Errorf("duplicate todo id %q", it.ID)
		}
		seen[it.ID] = struct{}{}
		it.Content = strings.TrimSpace(it.Content)
		if it.Content == "" {
			return fmt.Errorf("todo item %d content is required", i+1)
		}
		status, err := normalizeStatus(it.Status)
		if err != nil {
			return fmt.Errorf("todo item %d: %w", i+1, err)
		}
		it.Status = status
		it.Note = strings.TrimSpace(it.Note)
		if it.Status == "in_progress" {
			inProgress++
		}
	}
	if inProgress > 1 {
		return fmt.Errorf("todo list can contain at most one in_progress item")
	}
	return nil
}

func render(sessionID string, l list) string {
	var b strings.Builder
	fmt.Fprintf(&b, "session_id=%s\n", sessionID)
	fmt.Fprintf(&b, "todo_count=%d\n\n", len(l.Items))
	b.WriteString("## Todo\n\n")
	for i, it := range l.Items {
		fmt.Fprintf(&b, "- [%s] %d. %s", marker(it.Status), i+1, it.Content)
		if it.ID != fmt.Sprintf("%d", i+1) {
			fmt.Fprintf(&b, " {id=%s}", it.ID)
		}
		if it.Note != "" {
			fmt.Fprintf(&b, " - %s", it.Note)
		}
		b.WriteString("\n")
	}
	return b.String()
}

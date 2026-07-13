package filehistory

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/tool"
	toolfs "github.com/feimingxliu/ub/internal/tool/fs"
	"github.com/feimingxliu/ub/internal/workspace/paths"
)

// Backup records one tracked file's state at a snapshot boundary. Missing is
// true when the file did not exist in that version. Version is a monotonically
// increasing counter for the file within the session. BackupFileName is the
// path (relative to the backup directory) where the file content was saved
// before modification.
type Backup struct {
	BackupFileName string    `json:"backup_file_name,omitempty"`
	Missing        bool      `json:"missing,omitempty"`
	Version        int       `json:"version"`
	BackupTime     time.Time `json:"backup_time"`
}

// Snapshot captures the file state immediately before a user turn. It maps
// each tracked file's relative workspace path to its current Backup metadata.
// Snapshots are persisted as rollout events and are used by /rewind to restore
// the workspace to the state before a given user turn.
type Snapshot struct {
	Turn               int               `json:"turn"`
	TrackedFileBackups map[string]Backup `json:"tracked_file_backups"`
	Timestamp          time.Time         `json:"timestamp"`
}

// EventPayload is stored in rollout events. Updates replace the snapshot for
// the same turn after a file is first tracked during that turn. IsUpdate=true
// means this payload should replace the existing snapshot for this turn rather
// than append a new one.
type EventPayload struct {
	Snapshot Snapshot `json:"snapshot"`
	IsUpdate bool     `json:"is_update,omitempty"`
}

// Change describes a file that would change when rewinding to a snapshot.
// Path is the relative workspace path; Kind is "modified", "created", or
// "deleted" relative to the snapshot state.
type Change struct {
	Path string
	Kind string
}

// State is the in-memory file-history chain reconstructed from rollout events.
// Snapshots are ordered by turn (then timestamp). TrackedFiles records every
// file that has ever been tracked, so MakeSnapshot knows which files to back up
// even before the first actual backup.
type State struct {
	Snapshots    []Snapshot
	TrackedFiles map[string]struct{}
}

// Options configures a Manager. Rollout is the event writer used to persist
// new snapshots; Events is the existing event list loaded at session start for
// reconstructing previous state.
type Options struct {
	Workspace string
	SessionID string
	Rollout   rollout.Writer
	Events    []rollout.Event
}

// Manager owns checkpoint state for one session. It tracks which files the
// agent has touched, backs up their content before each user turn, and can
// reconstruct the file state at any previous turn for /rewind. Backups are
// stored under the ub state directory at state-root/file-history/<sessionID>/.
type Manager struct {
	mu        sync.Mutex
	workspace string
	sessionID string
	backupDir string
	rollout   rollout.Writer
	state     State
}

type pathTarget struct {
	Path string
	Cwd  string
}

// New constructs a Manager from existing rollout events.
func New(opts Options) (*Manager, error) {
	workspace := strings.TrimSpace(opts.Workspace)
	if workspace == "" {
		return nil, fmt.Errorf("file history workspace is empty")
	}
	sessionID := strings.TrimSpace(opts.SessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("file history session id is empty")
	}
	stateRoot, err := paths.StateRoot()
	if err != nil {
		return nil, err
	}
	return &Manager{
		workspace: filepath.Clean(workspace),
		sessionID: sessionID,
		backupDir: filepath.Join(stateRoot, "file-history", sessionID),
		rollout:   opts.Rollout,
		state:     StateFromEvents(opts.Events),
	}, nil
}

// StateFromEvents rebuilds file-history state from rollout events.
func StateFromEvents(events []rollout.Event) State {
	state := State{TrackedFiles: map[string]struct{}{}}
	indexByTurn := map[int]int{}
	for _, event := range events {
		if event.Type != rollout.TypeFileHistorySnapshot {
			continue
		}
		var payload EventPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			continue
		}
		snapshot := normalizeSnapshot(payload.Snapshot, event.Turn)
		if snapshot.Turn <= 0 {
			continue
		}
		if payload.IsUpdate {
			if idx, ok := indexByTurn[snapshot.Turn]; ok {
				state.Snapshots[idx] = snapshot
			} else {
				indexByTurn[snapshot.Turn] = len(state.Snapshots)
				state.Snapshots = append(state.Snapshots, snapshot)
			}
		} else {
			indexByTurn[snapshot.Turn] = len(state.Snapshots)
			state.Snapshots = append(state.Snapshots, snapshot)
		}
		for path := range snapshot.TrackedFileBackups {
			state.TrackedFiles[path] = struct{}{}
		}
	}
	sort.SliceStable(state.Snapshots, func(i, j int) bool {
		if state.Snapshots[i].Turn != state.Snapshots[j].Turn {
			return state.Snapshots[i].Turn < state.Snapshots[j].Turn
		}
		return state.Snapshots[i].Timestamp.Before(state.Snapshots[j].Timestamp)
	})
	return state
}

func normalizeSnapshot(snapshot Snapshot, eventTurn int) Snapshot {
	if snapshot.Turn <= 0 {
		snapshot.Turn = eventTurn
	}
	if snapshot.TrackedFileBackups == nil {
		snapshot.TrackedFileBackups = map[string]Backup{}
	}
	if snapshot.Timestamp.IsZero() {
		snapshot.Timestamp = time.Now().UTC()
	}
	normalized := make(map[string]Backup, len(snapshot.TrackedFileBackups))
	for path, backup := range snapshot.TrackedFileBackups {
		path = normalizeRel(path)
		if path == "" {
			continue
		}
		if backup.Version <= 0 {
			backup.Version = 1
		}
		if backup.BackupTime.IsZero() {
			backup.BackupTime = snapshot.Timestamp
		}
		normalized[path] = backup
	}
	snapshot.TrackedFileBackups = normalized
	return snapshot
}

// MakeSnapshot records the workspace state immediately before a user turn.
// It backs up all currently-tracked files to the backup directory (one file
// per version), writes a rollout event, and appends the snapshot to the
// in-memory state. Idempotent: calling MakeSnapshot for a turn that already
// has a snapshot is a no-op.
func (m *Manager) MakeSnapshot(ctx context.Context, turn int) error {
	if m == nil || turn <= 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.snapshotIndex(turn) >= 0 {
		return nil
	}
	backups := map[string]Backup{}
	last := m.lastSnapshot()
	for rel := range m.state.TrackedFiles {
		backup, err := m.backupForSnapshot(rel, last)
		if err != nil {
			continue
		}
		backups[rel] = backup
	}
	snapshot := Snapshot{
		Turn:               turn,
		TrackedFileBackups: backups,
		Timestamp:          time.Now().UTC(),
	}
	if err := m.recordSnapshot(ctx, snapshot, false); err != nil {
		return err
	}
	m.state.Snapshots = append(m.state.Snapshots, snapshot)
	return nil
}

// backupForSnapshot creates a Backup for a tracked file at the given turn,
// copying its current content to the backup directory if the file exists.
// Returns Missing=true when the file does not exist at this point.
func (m *Manager) backupForSnapshot(rel string, previous *Snapshot) (Backup, error) {
	rel = normalizeRel(rel)
	latest, hasLatest := Backup{}, false
	if previous != nil {
		latest, hasLatest = previous.TrackedFileBackups[rel]
	}
	nextVersion := 1
	if hasLatest && latest.Version > 0 {
		nextVersion = latest.Version + 1
	}
	abs, err := m.absForRel(rel)
	if err != nil {
		return Backup{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if hasLatest && latest.Missing {
				return latest, nil
			}
			return Backup{Missing: true, Version: nextVersion, BackupTime: time.Now().UTC()}, nil
		}
		return Backup{}, err
	}
	if info.IsDir() {
		return Backup{}, fmt.Errorf("%s is a directory", rel)
	}
	if hasLatest && !latest.Missing {
		changed, err := m.fileChanged(abs, latest)
		if err == nil && !changed {
			return latest, nil
		}
	}
	return m.createBackup(abs, rel, nextVersion)
}

// TrackTool inspects a tool call's arguments and registers any file paths
// that the tool may modify (write/edit/multiedit/apply_patch/bash rm) for file-history
// tracking. This must be called BEFORE the tool executes so the pre-edit
// content is captured in the current turn's snapshot.
func (m *Manager) TrackTool(ctx context.Context, name string, input json.RawMessage) error {
	if m == nil {
		return nil
	}
	targets := toolTargets(name, input)
	for _, target := range targets {
		if err := m.TrackPath(ctx, target.Path, target.Cwd); err != nil {
			continue
		}
	}
	return nil
}

// TrackPath captures one file's pre-edit contents in the current snapshot.
// If the file is already tracked in this snapshot, it is a no-op. The snapshot
// is updated in-place (IsUpdate=true) so the rollout records the latest state.
func (m *Manager) TrackPath(ctx context.Context, path, cwd string) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.currentSnapshot()
	if current == nil {
		return fmt.Errorf("file history snapshot missing")
	}
	abs, rel, err := m.resolveTarget(path, cwd)
	if err != nil {
		return err
	}
	if _, ok := current.TrackedFileBackups[rel]; ok {
		return nil
	}
	backup, err := m.createBackup(abs, rel, 1)
	if err != nil {
		return err
	}
	updated := cloneSnapshot(*current)
	updated.TrackedFileBackups[rel] = backup
	if err := m.recordSnapshot(ctx, updated, true); err != nil {
		return err
	}
	idx := m.snapshotIndex(updated.Turn)
	if idx >= 0 {
		m.state.Snapshots[idx] = updated
	}
	m.state.TrackedFiles[rel] = struct{}{}
	return nil
}

// ChangedFiles returns files that would change if the target snapshot were
// applied to the current workspace. Each Change includes the relative path
// and kind (modified/created/deleted). Returns nil if no snapshot exists for
// the turn or no files would change.
func (m *Manager) ChangedFiles(turn int) []Change {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	target := m.snapshotForTurn(turn)
	if target == nil {
		return nil
	}
	paths := make([]string, 0, len(m.state.TrackedFiles))
	for rel := range m.state.TrackedFiles {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	var out []Change
	for _, rel := range paths {
		backup, ok := m.backupForTarget(*target, rel)
		if !ok {
			continue
		}
		kind, changed := m.changeKind(rel, backup)
		if changed {
			out = append(out, Change{Path: rel, Kind: kind})
		}
	}
	return out
}

// Rewind applies the target snapshot to the workspace, restoring each tracked
// file to its state at that turn. Returns the list of changed files and a list
// of skipped files (with reasons) for files that could not be restored (e.g.
// missing backup, path outside workspace). Files that are already at the
// snapshot state are silently skipped.
func (m *Manager) Rewind(turn int) ([]Change, []string, error) {
	if m == nil {
		return nil, nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	target := m.snapshotForTurn(turn)
	if target == nil {
		return nil, nil, fmt.Errorf("file checkpoint for turn %d not found", turn)
	}
	paths := make([]string, 0, len(m.state.TrackedFiles))
	for rel := range m.state.TrackedFiles {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	var changed []Change
	var skipped []string
	for _, rel := range paths {
		backup, ok := m.backupForTarget(*target, rel)
		if !ok {
			skipped = append(skipped, rel+" (missing checkpoint)")
			continue
		}
		kind, didChange, err := m.applyBackup(rel, backup)
		if err != nil {
			skipped = append(skipped, rel+" ("+err.Error()+")")
			continue
		}
		if didChange {
			changed = append(changed, Change{Path: rel, Kind: kind})
		}
	}
	return changed, skipped, nil
}

// backupForTarget finds the Backup for a file at the target snapshot. If the
// file is not in the target snapshot directly, it searches later snapshots for
// the file's version-1 backup (the initial state before any edits in that turn).
// This handles the case where a file was first tracked mid-turn via TrackPath.
func (m *Manager) backupForTarget(target Snapshot, rel string) (Backup, bool) {
	if backup, ok := target.TrackedFileBackups[rel]; ok {
		return backup, true
	}
	for _, snapshot := range m.state.Snapshots {
		if snapshot.Turn < target.Turn {
			continue
		}
		backup, ok := snapshot.TrackedFileBackups[rel]
		if ok && backup.Version == 1 {
			return backup, true
		}
	}
	return Backup{}, false
}

// applyBackup restores a single file from its backup. If the backup marks the
// file as Missing (did not exist at snapshot time), the current file is removed.
// Otherwise the backup content is copied over the current file, preserving the
// original file execmode. Returns the change kind and whether the file actually
// changed (unchanged files are not rewritten).
func (m *Manager) applyBackup(rel string, backup Backup) (string, bool, error) {
	abs, err := m.absForRel(rel)
	if err != nil {
		return "", false, err
	}
	if backup.Missing {
		if _, err := os.Stat(abs); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return tool.KindCreate, false, nil
			}
			return "", false, err
		}
		if err := os.Remove(abs); err != nil {
			return "", false, err
		}
		return tool.KindCreate, true, nil
	}
	kind, changed := m.changeKind(rel, backup)
	if !changed {
		return kind, false, nil
	}
	backupPath := m.backupPath(backup)
	info, err := os.Stat(backupPath)
	if err != nil {
		return "", false, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", false, err
	}
	if err := copyFile(backupPath, abs); err != nil {
		return "", false, err
	}
	if err := os.Chmod(abs, info.Mode()); err != nil {
		return "", false, err
	}
	return kind, true, nil
}

// changeKind compares the current workspace file against its backup to
// determine what kind of change would occur if the backup were applied:
// KindCreate if the file was missing and now exists, KindDelete if it existed
// and is now gone, KindModify if content differs. Returns (kind, changed)
// where changed=false means the file is already at the backup state.
func (m *Manager) changeKind(rel string, backup Backup) (string, bool) {
	abs, err := m.absForRel(rel)
	if err != nil {
		return tool.KindModify, false
	}
	_, statErr := os.Stat(abs)
	if backup.Missing {
		return tool.KindCreate, statErr == nil
	}
	if errors.Is(statErr, os.ErrNotExist) {
		return tool.KindDelete, true
	}
	changed, err := m.fileChanged(abs, backup)
	if err != nil {
		return tool.KindModify, true
	}
	return tool.KindModify, changed
}

// createBackup copies a file's current content to the backup directory and
// returns a Backup metadata record. If the file does not exist, returns a
// Missing backup (no content is written). Directories produce an error.
// The backup file is named with a hashed path + version suffix.
func (m *Manager) createBackup(abs, rel string, version int) (Backup, error) {
	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Backup{Missing: true, Version: version, BackupTime: time.Now().UTC()}, nil
		}
		return Backup{}, err
	}
	if info.IsDir() {
		return Backup{}, fmt.Errorf("%s is a directory", rel)
	}
	name := backupFileName(rel, version)
	dst := filepath.Join(m.backupDir, name)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return Backup{}, err
	}
	if err := copyFile(abs, dst); err != nil {
		return Backup{}, err
	}
	if err := os.Chmod(dst, info.Mode()); err != nil {
		return Backup{}, err
	}
	return Backup{BackupFileName: name, Version: version, BackupTime: time.Now().UTC()}, nil
}

// fileChanged compares the current file against its backup. It first checks
// file mode and size as a fast path, then falls back to a full content hash
// comparison. A missing current file or missing backup is treated as changed.
func (m *Manager) fileChanged(abs string, backup Backup) (bool, error) {
	if backup.Missing {
		_, err := os.Stat(abs)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return true, err
	}
	backupPath := m.backupPath(backup)
	origInfo, origErr := os.Stat(abs)
	backupInfo, backupErr := os.Stat(backupPath)
	if errors.Is(origErr, os.ErrNotExist) || errors.Is(backupErr, os.ErrNotExist) {
		return !errors.Is(origErr, os.ErrNotExist) || !errors.Is(backupErr, os.ErrNotExist), nil
	}
	if origErr != nil {
		return true, origErr
	}
	if backupErr != nil {
		return true, backupErr
	}
	if origInfo.Mode() != backupInfo.Mode() || origInfo.Size() != backupInfo.Size() {
		return true, nil
	}
	orig, err := os.ReadFile(abs)
	if err != nil {
		return true, err
	}
	saved, err := os.ReadFile(backupPath)
	if err != nil {
		return true, err
	}
	return !bytes.Equal(orig, saved), nil
}

func (m *Manager) recordSnapshot(ctx context.Context, snapshot Snapshot, isUpdate bool) error {
	if m.rollout == nil {
		return nil
	}
	event, err := rollout.FileHistorySnapshot(m.sessionID, snapshot.Turn, EventPayload{
		Snapshot: snapshot,
		IsUpdate: isUpdate,
	})
	if err != nil {
		return err
	}
	return m.rollout.Append(ctx, event)
}

func (m *Manager) currentSnapshot() *Snapshot {
	if len(m.state.Snapshots) == 0 {
		return nil
	}
	return &m.state.Snapshots[len(m.state.Snapshots)-1]
}

func (m *Manager) lastSnapshot() *Snapshot {
	return m.currentSnapshot()
}

func (m *Manager) snapshotForTurn(turn int) *Snapshot {
	for i := len(m.state.Snapshots) - 1; i >= 0; i-- {
		if m.state.Snapshots[i].Turn == turn {
			return &m.state.Snapshots[i]
		}
	}
	return nil
}

func (m *Manager) snapshotIndex(turn int) int {
	for i := len(m.state.Snapshots) - 1; i >= 0; i-- {
		if m.state.Snapshots[i].Turn == turn {
			return i
		}
	}
	return -1
}

// resolveTarget resolves a tool path argument (which may be relative to cwd
// or absolute) to (abs, rel) where rel is relative to the workspace root.
// Paths outside the workspace are rejected to prevent tracking files in
// unrelated directories.
func (m *Manager) resolveTarget(path, cwd string) (string, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", "", fmt.Errorf("empty path")
	}
	base := m.workspace
	if strings.TrimSpace(cwd) != "" {
		absCwd, err := tool.Resolve(m.workspace, cwd)
		if err != nil {
			return "", "", err
		}
		base = absCwd
	}
	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(filepath.Join(base, filepath.FromSlash(path)))
	}
	rel, err := filepath.Rel(m.workspace, abs)
	if err != nil {
		return "", "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("path outside workspace")
	}
	return abs, filepath.ToSlash(rel), nil
}

func (m *Manager) absForRel(rel string) (string, error) {
	rel = normalizeRel(rel)
	if rel == "" {
		return "", fmt.Errorf("empty path")
	}
	return tool.Resolve(m.workspace, rel)
}

func (m *Manager) backupPath(backup Backup) string {
	return filepath.Join(m.backupDir, backup.BackupFileName)
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.TrackedFileBackups = cloneBackups(snapshot.TrackedFileBackups)
	return snapshot
}

func cloneBackups(in map[string]Backup) map[string]Backup {
	out := make(map[string]Backup, len(in)+1)
	for key, value := range in {
		out[key] = value
	}
	return out
}

func normalizeRel(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	path = strings.TrimPrefix(path, "./")
	return path
}

func backupFileName(rel string, version int) string {
	sum := sha256.Sum256([]byte(normalizeRel(rel)))
	return hex.EncodeToString(sum[:]) + fmt.Sprintf("@v%d", version)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func toolTargets(name string, input json.RawMessage) []pathTarget {
	switch strings.TrimSpace(name) {
	case "write", "edit":
		var args struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(input, &args); err != nil || strings.TrimSpace(args.Path) == "" {
			return nil
		}
		return []pathTarget{{Path: args.Path}}
	case "multiedit":
		return multiEditTargets(input)
	case "apply_patch":
		var args struct {
			Patch string `json:"patch"`
		}
		if err := json.Unmarshal(input, &args); err != nil || strings.TrimSpace(args.Patch) == "" {
			return nil
		}
		paths, err := toolfs.PatchPaths(args.Patch)
		if err != nil {
			return nil
		}
		out := make([]pathTarget, 0, len(paths))
		for _, path := range paths {
			out = append(out, pathTarget{Path: path})
		}
		return out
	case "bash":
		var args struct {
			Command string `json:"command"`
			Cwd     string `json:"cwd,omitempty"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return nil
		}
		paths := rmDeletedPaths(args.Command)
		out := make([]pathTarget, 0, len(paths))
		for _, path := range paths {
			out = append(out, pathTarget{Path: path, Cwd: args.Cwd})
		}
		return out
	default:
		return nil
	}
}

func multiEditTargets(input json.RawMessage) []pathTarget {
	var outer struct {
		Edits json.RawMessage `json:"edits"`
	}
	if err := json.Unmarshal(input, &outer); err != nil {
		return nil
	}
	raw := outer.Edits
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err == nil {
		raw = []byte(strings.TrimSpace(encoded))
	}
	var edits []struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &edits); err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []pathTarget
	for _, edit := range edits {
		path := strings.TrimSpace(edit.Path)
		if path == "" {
			continue
		}
		key := normalizeRel(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, pathTarget{Path: path})
	}
	return out
}

func rmDeletedPaths(command string) []string {
	words, ok := shellWords(command)
	if !ok {
		return nil
	}
	var out []string
	for i := 0; i < len(words); i++ {
		token := words[i]
		if token == "cd" {
			return nil
		}
		if token == "rm" {
			paths, next := rmPathsFromWords(words, i+1)
			out = append(out, paths...)
			i = next
			continue
		}
		if token == "git" && i+1 < len(words) && words[i+1] == "rm" {
			paths, next := rmPathsFromWords(words, i+2)
			out = append(out, paths...)
			i = next
		}
	}
	return out
}

func rmPathsFromWords(words []string, start int) ([]string, int) {
	var paths []string
	afterDoubleDash := false
	i := start
	for ; i < len(words); i++ {
		token := words[i]
		if shellSeparator(token) {
			break
		}
		if !afterDoubleDash && token == "--" {
			afterDoubleDash = true
			continue
		}
		if !afterDoubleDash && strings.HasPrefix(token, "-") {
			continue
		}
		if safeLiteralShellPath(token) {
			paths = append(paths, token)
		}
	}
	return paths, i
}

func shellWords(command string) ([]string, bool) {
	var words []string
	var b strings.Builder
	var quote rune
	flush := func() {
		if b.Len() == 0 {
			return
		}
		words = append(words, b.String())
		b.Reset()
	}
	for i := 0; i < len(command); {
		r, size := utf8.DecodeRuneInString(command[i:])
		if quote != 0 {
			if r == quote {
				quote = 0
				i += size
				continue
			}
			if r == '\\' && quote == '"' {
				i += size
				if i >= len(command) {
					return nil, false
				}
				next, nextSize := utf8.DecodeRuneInString(command[i:])
				b.WriteRune(next)
				i += nextSize
				continue
			}
			b.WriteRune(r)
			i += size
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
			i += size
		case '\\':
			i += size
			if i >= len(command) {
				return nil, false
			}
			next, nextSize := utf8.DecodeRuneInString(command[i:])
			b.WriteRune(next)
			i += nextSize
		case ' ', '\t', '\n', '\r':
			flush()
			i += size
		case ';':
			flush()
			words = append(words, ";")
			i += size
		case '&':
			flush()
			if strings.HasPrefix(command[i:], "&&") {
				words = append(words, "&&")
				i += 2
				continue
			}
			return nil, false
		case '|':
			flush()
			if strings.HasPrefix(command[i:], "||") {
				words = append(words, "||")
				i += 2
				continue
			}
			return nil, false
		default:
			b.WriteRune(r)
			i += size
		}
	}
	if quote != 0 {
		return nil, false
	}
	flush()
	return words, true
}

func shellSeparator(token string) bool {
	switch token {
	case ";", "&&", "||":
		return true
	default:
		return false
	}
}

func safeLiteralShellPath(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	for _, r := range path {
		switch r {
		case '$', '*', '?', '[', ']', '{', '}', '`', '<', '>', '|', '&', ';':
			return false
		}
	}
	return true
}

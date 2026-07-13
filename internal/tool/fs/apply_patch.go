package fs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	udiff "github.com/aymanbagabas/go-udiff"
	"github.com/google/uuid"
	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/tool"
)

const (
	patchBeginMarker = "*** Begin Patch"
	patchEndMarker   = "*** End Patch"
	patchEOFMarker   = "*** End of File"
)

// patchReadFileFn and patchWriteFileFn are overridden by tests to exercise
// the preview binding and rollback paths without weakening production I/O.
var patchReadFileFn = func(root *os.Root, name string) ([]byte, error) {
	return root.ReadFile(name)
}

var patchWriteFileFn = func(file *os.File, content []byte) error {
	for len(content) > 0 {
		n, err := file.Write(content)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		content = content[n:]
	}
	return nil
}

const maxCachedPatchPreviews = 64

type applyPatchArgs struct {
	Patch string `json:"patch" jsonschema:"required,description=Full *** Begin Patch ... *** End Patch envelope. Prefer this for multi-line, structural, or multi-file edits. Update hunks use @@ followed by context lines (space), removals (-), and additions (+)."`
}

type patchOpKind string

const (
	patchAdd    patchOpKind = "add"
	patchUpdate patchOpKind = "update"
	patchDelete patchOpKind = "delete"
)

type patchHunk struct {
	anchor string
	old    []string
	new    []string
	eof    bool
	line   int
}

type patchOperation struct {
	kind   patchOpKind
	path   string
	moveTo string
	add    []string
	hunks  []patchHunk
	line   int
}

type applyPatchTool struct {
	root      string
	notifier  ChangeNotifier
	schema    *jsonschema.Schema
	previewMu sync.Mutex
	previews  map[string]cachedPatchPlan
}

type cachedPatchPlan struct {
	raw  string
	plan applyPatchPlan
}

func newApplyPatchTool(root string) *applyPatchTool {
	return newApplyPatchToolWithNotifier(root, nil)
}

func newApplyPatchToolWithNotifier(root string, notifier ChangeNotifier) *applyPatchTool {
	return &applyPatchTool{
		root:     root,
		notifier: notifier,
		schema:   jsonschema.Reflect(&applyPatchArgs{}),
		previews: make(map[string]cachedPatchPlan),
	}
}

func (t *applyPatchTool) Name() string { return "apply_patch" }
func (t *applyPatchTool) Description() string {
	return "Apply one atomic, context-aware file patch. Prefer this for multi-line, structural, or multi-file edits: pass a full *** Begin Patch ... *** End Patch envelope with *** Add File, *** Update File, or *** Delete File sections. Update hunks use @@ plus unchanged context lines (space), removed lines (-), and added lines (+). Include enough unchanged context to identify exactly one location; ambiguous or stale hunks are rejected instead of guessed. Preview shows the complete diff before permission. For tiny exact substitutions, edit and multiedit remain available."
}
func (t *applyPatchTool) Schema() *jsonschema.Schema { return t.schema }
func (t *applyPatchTool) Risk() tool.Risk            { return tool.RiskWrite }

func parseApplyPatchArgs(raw json.RawMessage) (applyPatchArgs, error) {
	var args applyPatchArgs
	if err := tool.DecodeArgs("apply_patch", raw, &args); err != nil {
		return args, err
	}
	if strings.TrimSpace(args.Patch) == "" {
		return args, fmt.Errorf("apply_patch: patch is required")
	}
	return args, nil
}

func parsePatch(input string) ([]patchOperation, error) {
	lines := strings.Split(normalizePatchLineEndings(input), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) < 2 || lines[0] != patchBeginMarker || lines[len(lines)-1] != patchEndMarker {
		return nil, fmt.Errorf("apply_patch: patch must start with %q and end with %q", patchBeginMarker, patchEndMarker)
	}

	var ops []patchOperation
	for i := 1; i < len(lines)-1; {
		line := lines[i]
		op, next, err := parsePatchOperation(lines, i, len(lines)-1)
		if err != nil {
			return nil, err
		}
		ops = append(ops, op)
		i = next
		if i <= 1 || line == patchEndMarker {
			return nil, fmt.Errorf("apply_patch: invalid patch operation at line %d", i+1)
		}
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("apply_patch: empty patch")
	}
	return ops, nil
}

func parsePatchOperation(lines []string, start, end int) (patchOperation, int, error) {
	line := lines[start]
	op := patchOperation{line: start + 1}
	switch {
	case strings.HasPrefix(line, "*** Add File: "):
		op.kind = patchAdd
		op.path = strings.TrimSpace(strings.TrimPrefix(line, "*** Add File: "))
		if op.path == "" {
			return op, start, fmt.Errorf("apply_patch: add file path is required at line %d", start+1)
		}
		i := start + 1
		for i < end && !strings.HasPrefix(lines[i], "*** ") {
			if !strings.HasPrefix(lines[i], "+") {
				return op, start, fmt.Errorf("apply_patch: add file content must start with + at line %d", i+1)
			}
			op.add = append(op.add, strings.TrimPrefix(lines[i], "+"))
			i++
		}
		return op, i, nil
	case strings.HasPrefix(line, "*** Delete File: "):
		op.kind = patchDelete
		op.path = strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File: "))
		if op.path == "" {
			return op, start, fmt.Errorf("apply_patch: delete file path is required at line %d", start+1)
		}
		if start+1 < end && !strings.HasPrefix(lines[start+1], "*** ") {
			return op, start, fmt.Errorf("apply_patch: delete file must not contain content at line %d", start+2)
		}
		return op, start + 1, nil
	case strings.HasPrefix(line, "*** Update File: "):
		op.kind = patchUpdate
		op.path = strings.TrimSpace(strings.TrimPrefix(line, "*** Update File: "))
		if op.path == "" {
			return op, start, fmt.Errorf("apply_patch: update file path is required at line %d", start+1)
		}
		i := start + 1
		if i < end && strings.HasPrefix(lines[i], "*** Move to: ") {
			op.moveTo = strings.TrimSpace(strings.TrimPrefix(lines[i], "*** Move to: "))
			if op.moveTo == "" {
				return op, start, fmt.Errorf("apply_patch: move destination is required at line %d", i+1)
			}
			i++
		}
		for i < end && !strings.HasPrefix(lines[i], "*** ") {
			if !strings.HasPrefix(lines[i], "@@") {
				return op, start, fmt.Errorf("apply_patch: expected @@ hunk at line %d", i+1)
			}
			hunk, next, err := parsePatchHunk(lines, i, end)
			if err != nil {
				return op, start, err
			}
			op.hunks = append(op.hunks, hunk)
			i = next
		}
		if len(op.hunks) == 0 {
			return op, start, fmt.Errorf("apply_patch: update %s has no hunks", op.path)
		}
		return op, i, nil
	default:
		return op, start, fmt.Errorf("apply_patch: unknown operation at line %d: %s", start+1, line)
	}
}

func parsePatchHunk(lines []string, start, end int) (patchHunk, int, error) {
	hunk := patchHunk{anchor: strings.TrimSpace(strings.TrimPrefix(lines[start], "@@")), line: start + 1}
	i := start + 1
	changed := false
	for i < end && !strings.HasPrefix(lines[i], "@@") && (lines[i] == patchEOFMarker || !strings.HasPrefix(lines[i], "*** ")) {
		line := lines[i]
		if line == patchEOFMarker {
			hunk.eof = true
			i++
			break
		}
		if line == "" {
			return hunk, start, fmt.Errorf("apply_patch: hunk line must start with space, +, or - at line %d", i+1)
		}
		switch line[0] {
		case ' ':
			hunk.old = append(hunk.old, line[1:])
			hunk.new = append(hunk.new, line[1:])
		case '-':
			hunk.old = append(hunk.old, line[1:])
			changed = true
		case '+':
			hunk.new = append(hunk.new, line[1:])
			changed = true
		default:
			return hunk, start, fmt.Errorf("apply_patch: hunk line must start with space, +, or - at line %d", i+1)
		}
		i++
	}
	if !changed {
		return hunk, start, fmt.Errorf("apply_patch: hunk at line %d makes no change", start+1)
	}
	return hunk, i, nil
}

func normalizePatchLineEndings(input string) string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	return strings.ReplaceAll(input, "\r", "\n")
}

// PatchPaths returns all raw workspace paths that a valid patch can modify.
// It is shared with file checkpoint tracking so the checkpoint uses the same
// grammar as the writer. Paths are intentionally not resolved here.
func PatchPaths(input string) ([]string, error) {
	ops, err := parsePatch(input)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(ops)*2)
	var paths []string
	for _, op := range ops {
		for _, path := range []string{op.path, op.moveTo} {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
			paths = append(paths, path)
		}
	}
	return paths, nil
}

type patchText struct {
	bom      string
	lines    []string
	eol      string
	trailing bool
}

func parsePatchText(content []byte) patchText {
	text := string(content)
	parsed := patchText{eol: "\n"}
	if strings.HasPrefix(text, "\ufeff") {
		parsed.bom = "\ufeff"
		text = strings.TrimPrefix(text, parsed.bom)
	}
	if strings.Contains(text, "\r\n") {
		parsed.eol = "\r\n"
	}
	text = normalizePatchLineEndings(text)
	parsed.trailing = strings.HasSuffix(text, "\n")
	if parsed.trailing {
		text = strings.TrimSuffix(text, "\n")
	}
	if text != "" {
		parsed.lines = strings.Split(text, "\n")
	}
	return parsed
}

func newPatchText(lines []string) patchText {
	return patchText{lines: append([]string(nil), lines...), eol: "\n", trailing: len(lines) > 0}
}

func (text patchText) bytes() []byte {
	body := strings.Join(text.lines, text.eol)
	if len(text.lines) > 0 && text.trailing {
		body += text.eol
	}
	return []byte(text.bom + body)
}

func (text patchText) apply(hunks []patchHunk, path string) (patchText, error) {
	searchStart := 0
	for index, hunk := range hunks {
		start, err := findHunk(text.lines, hunk, searchStart, path, index+1)
		if err != nil {
			return patchText{}, err
		}
		updated := make([]string, 0, len(text.lines)-len(hunk.old)+len(hunk.new))
		updated = append(updated, text.lines[:start]...)
		updated = append(updated, hunk.new...)
		updated = append(updated, text.lines[start+len(hunk.old):]...)
		text.lines = updated
		searchStart = start + len(hunk.new)
	}
	return text, nil
}

func findHunk(lines []string, hunk patchHunk, searchStart int, path string, hunkNumber int) (int, error) {
	if len(hunk.old) == 0 {
		if !hunk.eof {
			return 0, fmt.Errorf("apply_patch: %s hunk %d has no expected context; re-read the file and include context or use *** End of File", path, hunkNumber)
		}
		return len(lines), nil
	}
	var matches []int
	for start := searchStart; start+len(hunk.old) <= len(lines); start++ {
		if hunk.anchor != "" && !anchorBefore(lines, hunk.anchor, searchStart, start) {
			continue
		}
		if !equalPatchLines(lines[start:start+len(hunk.old)], hunk.old) {
			continue
		}
		if hunk.eof && start+len(hunk.old) != len(lines) {
			continue
		}
		matches = append(matches, start)
	}
	switch len(matches) {
	case 0:
		return 0, fmt.Errorf("apply_patch: %s hunk %d did not match; re-read the file and include exact context", path, hunkNumber)
	case 1:
		return matches[0], nil
	default:
		return 0, fmt.Errorf("apply_patch: %s hunk %d has multiple matches at lines %s; add more context and retry", path, hunkNumber, formatPatchMatchLines(matches))
	}
}

func anchorBefore(lines []string, anchor string, lower, start int) bool {
	for index := lower; index <= start && index < len(lines); index++ {
		if lines[index] == anchor {
			return true
		}
	}
	return false
}

func equalPatchLines(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func formatPatchMatchLines(matches []int) string {
	const maxListed = 5
	items := make([]string, 0, min(len(matches), maxListed))
	for _, match := range matches[:min(len(matches), maxListed)] {
		items = append(items, fmt.Sprintf("%d", match+1))
	}
	if len(matches) > maxListed {
		items = append(items, "...")
	}
	return strings.Join(items, ", ")
}

type patchSnapshot struct {
	abs    string
	rel    string
	name   string
	exists bool
	data   []byte
	mode   os.FileMode
}

type patchPlanFile struct {
	kind    patchOpKind
	source  patchSnapshot
	target  patchSnapshot
	after   []byte
	display string
}

func (file patchPlanFile) kindForResult() string {
	switch file.kind {
	case patchAdd:
		return tool.KindCreate
	case patchDelete:
		return tool.KindDelete
	default:
		return tool.KindModify
	}
}

func (file patchPlanFile) diff() string {
	before := string(file.source.data)
	pathBefore := file.source.rel
	pathAfter := file.target.rel
	if file.kind == patchAdd {
		before = ""
		pathBefore = file.target.rel
	}
	if file.kind == patchDelete {
		pathAfter = file.source.rel
	}
	return udiff.Unified(pathBefore, pathAfter, before, string(file.after))
}

type applyPatchPlan struct {
	files     []patchPlanFile
	snapshots []patchSnapshot
}

func (t *applyPatchTool) plan(raw json.RawMessage) (applyPatchPlan, error) {
	args, err := parseApplyPatchArgs(raw)
	if err != nil {
		return applyPatchPlan{}, err
	}
	ops, err := parsePatch(args.Patch)
	if err != nil {
		return applyPatchPlan{}, err
	}
	workspace, err := os.OpenRoot(t.root)
	if err != nil {
		return applyPatchPlan{}, fmt.Errorf("apply_patch: open workspace root: %w", err)
	}
	defer workspace.Close()

	used := map[string]struct{}{}
	snapshots := map[string]patchSnapshot{}
	load := func(path string, requireExists bool) (patchSnapshot, error) {
		abs, err := resolve(t.root, path)
		if err != nil {
			return patchSnapshot{}, err
		}
		if existing, ok := snapshots[abs]; ok {
			if requireExists && !existing.exists {
				return patchSnapshot{}, fmt.Errorf("%s does not exist", path)
			}
			return existing, nil
		}
		rel, _ := relToRoot(t.root, abs)
		name := filepath.FromSlash(rel)
		info, err := workspace.Stat(name)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				snapshot := patchSnapshot{abs: abs, rel: rel, name: name}
				snapshots[abs] = snapshot
				if requireExists {
					return patchSnapshot{}, fmt.Errorf("%s does not exist", rel)
				}
				return snapshot, nil
			}
			return patchSnapshot{}, patchRootError("stat", rel, err)
		}
		if !info.Mode().IsRegular() {
			return patchSnapshot{}, fmt.Errorf("%s is not a regular file", rel)
		}
		data, err := patchReadFileFn(workspace, name)
		if err != nil {
			return patchSnapshot{}, patchRootError("read", rel, err)
		}
		snapshot := patchSnapshot{abs: abs, rel: rel, name: name, exists: true, data: data, mode: info.Mode()}
		snapshots[abs] = snapshot
		return snapshot, nil
	}
	claim := func(abs, rel string) error {
		if _, exists := used[abs]; exists {
			return fmt.Errorf("conflicting patch operations target %s", rel)
		}
		used[abs] = struct{}{}
		return nil
	}

	plan := applyPatchPlan{}
	for _, op := range ops {
		source, err := load(op.path, op.kind != patchAdd)
		if err != nil {
			return applyPatchPlan{}, fmt.Errorf("apply_patch: %w", err)
		}
		if err := claim(source.abs, source.rel); err != nil {
			return applyPatchPlan{}, fmt.Errorf("apply_patch: %w", err)
		}
		target := source
		if op.kind == patchAdd {
			if source.exists {
				return applyPatchPlan{}, fmt.Errorf("apply_patch: add target %s already exists", source.rel)
			}
			target = source
		} else if op.moveTo != "" {
			target, err = load(op.moveTo, false)
			if err != nil {
				return applyPatchPlan{}, fmt.Errorf("apply_patch: %w", err)
			}
			if target.exists {
				return applyPatchPlan{}, fmt.Errorf("apply_patch: move destination %s already exists", target.rel)
			}
			if err := claim(target.abs, target.rel); err != nil {
				return applyPatchPlan{}, fmt.Errorf("apply_patch: %w", err)
			}
		}

		file := patchPlanFile{kind: op.kind, source: source, target: target, display: target.rel}
		switch op.kind {
		case patchAdd:
			file.after = newPatchText(op.add).bytes()
		case patchUpdate:
			after, err := parsePatchText(source.data).apply(op.hunks, source.rel)
			if err != nil {
				return applyPatchPlan{}, err
			}
			file.after = after.bytes()
		case patchDelete:
			file.after = nil
			file.display = source.rel
		}
		plan.files = append(plan.files, file)
	}
	for _, snapshot := range snapshots {
		plan.snapshots = append(plan.snapshots, snapshot)
	}
	sort.Slice(plan.files, func(i, j int) bool { return plan.files[i].display < plan.files[j].display })
	sort.Slice(plan.snapshots, func(i, j int) bool { return plan.snapshots[i].rel < plan.snapshots[j].rel })
	return plan, nil
}

func (t *applyPatchTool) Preview(ctx context.Context, raw json.RawMessage) (tool.Preview, error) {
	plan, err := t.plan(raw)
	if err != nil {
		return tool.Preview{}, err
	}
	t.cachePreview(ctx, raw, plan)
	files := make([]tool.FileDiff, len(plan.files))
	for index, file := range plan.files {
		files[index] = tool.FileDiff{Path: file.display, Kind: file.kindForResult(), UnifiedDiff: file.diff()}
	}
	return tool.Preview{Summary: fmt.Sprintf("Apply patch to %d file(s)", len(files)), Files: files}, nil
}

func (t *applyPatchTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	plan, ok := t.takePreview(ctx, raw)
	if !ok {
		var err error
		plan, err = t.plan(raw)
		if err != nil {
			return tool.Result{}, err
		}
	}
	workspace, err := os.OpenRoot(t.root)
	if err != nil {
		return tool.Result{}, fmt.Errorf("apply_patch: open workspace root: %w", err)
	}
	defer workspace.Close()
	if err := checkPatchSnapshots(workspace, plan.snapshots); err != nil {
		return tool.Result{}, err
	}
	if err := commitPatchPlan(workspace, plan); err != nil {
		return tool.Result{}, err
	}

	changes := make([]tool.FileChange, len(plan.files))
	counts := map[patchOpKind]int{}
	var notifySuffix strings.Builder
	for index, file := range plan.files {
		changes[index] = tool.FileChange{Path: file.display, Kind: file.kindForResult(), UnifiedDiff: file.diff()}
		counts[file.kind]++
		if file.kind != patchDelete {
			notifySuffix.WriteString(notifyChanged(ctx, t.notifier, file.target.abs))
		}
	}
	return tool.Result{
		Content: fmt.Sprintf("apply_patch: %d file(s) (%d create, %d modify, %d delete)%s", len(changes), counts[patchAdd], counts[patchUpdate], counts[patchDelete], notifySuffix.String()),
		Files:   changes,
	}, nil
}

func (t *applyPatchTool) cachePreview(ctx context.Context, raw json.RawMessage, plan applyPatchPlan) {
	toolUseID := tool.ToolUseIDFromContext(ctx)
	if toolUseID == "" {
		return
	}
	t.previewMu.Lock()
	defer t.previewMu.Unlock()
	if len(t.previews) >= maxCachedPatchPreviews {
		for key := range t.previews {
			delete(t.previews, key)
			break
		}
	}
	t.previews[toolUseID] = cachedPatchPlan{raw: string(raw), plan: plan}
}

func (t *applyPatchTool) takePreview(ctx context.Context, raw json.RawMessage) (applyPatchPlan, bool) {
	toolUseID := tool.ToolUseIDFromContext(ctx)
	if toolUseID == "" {
		return applyPatchPlan{}, false
	}
	t.previewMu.Lock()
	defer t.previewMu.Unlock()
	cached, ok := t.previews[toolUseID]
	if !ok {
		return applyPatchPlan{}, false
	}
	delete(t.previews, toolUseID)
	if cached.raw != string(raw) {
		return applyPatchPlan{}, false
	}
	return cached.plan, true
}

func checkPatchSnapshots(workspace *os.Root, snapshots []patchSnapshot) error {
	for _, snapshot := range snapshots {
		info, err := workspace.Stat(snapshot.name)
		if snapshot.exists {
			if err != nil {
				if patchRootEscaped(err) {
					return fmt.Errorf("apply_patch: %s is outside workspace root: %w", snapshot.rel, err)
				}
				return fmt.Errorf("apply_patch: %s changed on disk since preview: %w", snapshot.rel, err)
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("apply_patch: %s changed on disk since preview", snapshot.rel)
			}
			current, err := patchReadFileFn(workspace, snapshot.name)
			if err != nil {
				if patchRootEscaped(err) {
					return fmt.Errorf("apply_patch: %s is outside workspace root: %w", snapshot.rel, err)
				}
				return fmt.Errorf("apply_patch: re-read %s: %w", snapshot.rel, err)
			}
			if string(current) != string(snapshot.data) {
				return fmt.Errorf("apply_patch: %s changed on disk since preview", snapshot.rel)
			}
			continue
		}
		if err == nil {
			return fmt.Errorf("apply_patch: target %s already exists", snapshot.rel)
		}
		if patchRootEscaped(err) {
			return fmt.Errorf("apply_patch: target %s is outside workspace root: %w", snapshot.rel, err)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("apply_patch: stat target %s: %w", snapshot.rel, err)
		}
	}
	return nil
}

func commitPatchPlan(workspace *os.Root, plan applyPatchPlan) error {
	for _, file := range plan.files {
		var err error
		switch file.kind {
		case patchAdd:
			err = writePatchFile(workspace, file.target.name, file.after, 0o644)
		case patchUpdate:
			mode := file.source.mode.Perm()
			if file.target.abs == file.source.abs {
				err = writePatchFile(workspace, file.source.name, file.after, mode)
			} else {
				err = writePatchFile(workspace, file.target.name, file.after, mode)
				if err == nil {
					err = workspace.Remove(file.source.name)
				}
			}
		case patchDelete:
			err = workspace.Remove(file.source.name)
		}
		if err == nil {
			continue
		}
		rollbackErr := restorePatchSnapshots(workspace, plan.snapshots)
		if rollbackErr != nil {
			return fmt.Errorf("apply_patch: %s: %w (rollback failed: %v)", file.display, err, rollbackErr)
		}
		return fmt.Errorf("apply_patch: %s: %w", file.display, err)
	}
	return nil
}

func patchRootError(op, rel string, err error) error {
	if patchRootEscaped(err) {
		return fmt.Errorf("%s %s is outside workspace root: %w", op, rel, err)
	}
	return fmt.Errorf("%s %s: %w", op, rel, err)
}

func patchRootEscaped(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "escape")
}

func writePatchFile(workspace *os.Root, name string, content []byte, mode os.FileMode) (err error) {
	parent := filepath.Dir(name)
	if parent != "." {
		if err := workspace.MkdirAll(parent, 0o755); err != nil {
			return err
		}
	}

	var tempName string
	var file *os.File
	for range 16 {
		tempName = filepath.Join(parent, ".ub-apply-patch-"+uuid.NewString())
		file, err = workspace.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		break
	}
	if file == nil {
		return fmt.Errorf("create temporary patch file after 16 attempts")
	}
	keepTemp := true
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
		if keepTemp {
			_ = workspace.Remove(tempName)
		}
	}()

	if err := patchWriteFileFn(file, content); err != nil {
		return err
	}
	if err := file.Chmod(mode.Perm()); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	closed = true
	if err := workspace.Rename(tempName, name); err != nil {
		return err
	}
	keepTemp = false
	return nil
}

func restorePatchSnapshots(workspace *os.Root, snapshots []patchSnapshot) error {
	var errs []error
	for _, snapshot := range snapshots {
		if snapshot.exists {
			if err := writePatchFile(workspace, snapshot.name, snapshot.data, snapshot.mode.Perm()); err != nil {
				errs = append(errs, fmt.Errorf("restore %s: %w", snapshot.rel, err))
			}
			continue
		}
		if err := workspace.Remove(snapshot.name); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove %s: %w", snapshot.rel, err))
		}
	}
	return errors.Join(errs...)
}

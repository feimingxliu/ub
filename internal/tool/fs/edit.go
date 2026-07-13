package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	udiff "github.com/aymanbagabas/go-udiff"
	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/tool"
)

// readFileFn is overridden in tests to exercise the TOCTOU guard in
// (*editTool).Execute. The first call returns the "before" snapshot,
// the second returns the (possibly mutated) current content.
var readFileFn = os.ReadFile

type editArgs struct {
	Path       string       `json:"path"        jsonschema:"required,description=Path relative to workspace root."`
	Old        string       `json:"old,omitempty" jsonschema:"description=Exact substring to replace, including tabs, spaces, and line endings. Required unless start_line is set. With start_line, old anchors the selected complete lines when provided; include it for multi-line replacements."`
	New        string       `json:"new"         jsonschema:"required,description=Replacement text. With start_line, this replaces complete lines; omit a trailing newline to preserve the replaced range's line structure. Multi-line line edits require old."`
	ReplaceAll tool.BoolArg `json:"replace_all,omitempty" jsonschema:"description=Replace all matches when true. Defaults to false."`
	StartLine  tool.IntArg  `json:"start_line,omitempty" jsonschema:"description=1-based first line to replace. When set, edit replaces complete lines. old may be omitted only for single-line line edits."`
	EndLine    tool.IntArg  `json:"end_line,omitempty"   jsonschema:"description=1-based last line to replace, inclusive. Defaults to start_line."`
}

type editTool struct {
	root     string
	notifier ChangeNotifier
	schema   *jsonschema.Schema
}

func newEditTool(root string) *editTool {
	return newEditToolWithNotifier(root, nil)
}

func newEditToolWithNotifier(root string, notifier ChangeNotifier) *editTool {
	return &editTool{
		root:     root,
		notifier: notifier,
		schema:   jsonschema.Reflect(&editArgs{}),
	}
}

func (t *editTool) Name() string { return "edit" }
func (t *editTool) Description() string {
	return "Replace text inside a workspace file. Prefer exact old/new replacement for targeted edits; old must match exactly, including tabs, spaces, and line endings. If exact old is hard to reconstruct from numbered read output, use start_line/end_line to replace complete lines by line number. In line mode, provide old for multi-line replacements or function/block moves so stale line numbers cannot silently edit the wrong place. If old is not found or the line anchor mismatches, re-read a narrow range around the target and retry with exact text. Prefer this over bash/sed/python for file edits."
}
func (t *editTool) Schema() *jsonschema.Schema { return t.schema }
func (t *editTool) Risk() tool.Risk            { return tool.RiskWrite }

func (t *editTool) parseAndResolve(raw json.RawMessage) (editArgs, string, error) {
	var a editArgs
	if err := tool.DecodeArgs("edit", raw, &a); err != nil {
		return a, "", err
	}
	if a.Path == "" {
		return a, "", fmt.Errorf("edit: path is required")
	}
	if !a.hasLineRange() && a.Old == "" {
		return a, "", fmt.Errorf("edit: old is required")
	}
	if a.hasLineRange() && bool(a.ReplaceAll) {
		return a, "", fmt.Errorf("edit: replace_all cannot be used with start_line")
	}
	abs, err := resolve(t.root, a.Path)
	if err != nil {
		return a, "", err
	}
	return a, abs, nil
}

func (a editArgs) hasLineRange() bool {
	return int(a.StartLine) > 0 || int(a.EndLine) > 0
}

// applyEdit returns the new file content and the number of replacements.
// It returns an error if old is missing or there are multiple matches
// without replace_all set. When a line range is specified, applyLineEdit
// handles the replacement by line number instead of string matching.
func applyEdit(content string, a editArgs) (string, int, error) {
	if a.hasLineRange() {
		return applyLineEdit(content, a)
	}
	count := strings.Count(content, a.Old)
	switch {
	case count == 0:
		return "", 0, editOldNotFoundError(content, a.Old)
	case count > 1 && !bool(a.ReplaceAll):
		return "", 0, fmt.Errorf("edit: %d matches, set replace_all=true to replace all", count)
	}
	n := 1
	if bool(a.ReplaceAll) {
		n = -1
	}
	return strings.Replace(content, a.Old, a.New, n), count, nil
}

type lineSpan struct {
	start int
	end   int
}

// applyLineEdit replaces a range of complete lines (1-based, inclusive) in
// the content. When old is provided, it is used as an anchor: the existing
// lines must contain old as a substring, otherwise the edit is rejected to
// prevent stale line numbers from silently editing the wrong location.
// Without old, only single-line edits are allowed (to reduce the risk of
// silently replacing the wrong lines).
func applyLineEdit(content string, a editArgs) (string, int, error) {
	startLine := int(a.StartLine)
	endLine := int(a.EndLine)
	if startLine <= 0 {
		return "", 0, fmt.Errorf("edit: start_line is required when end_line is set")
	}
	if endLine <= 0 {
		endLine = startLine
	}
	if endLine < startLine {
		return "", 0, fmt.Errorf("edit: end_line must be greater than or equal to start_line")
	}
	spans := contentLineSpans(content)
	if startLine > len(spans) || endLine > len(spans) {
		return "", 0, fmt.Errorf("edit: line range %d-%d is outside file with %d line(s)", startLine, endLine, len(spans))
	}

	start := spans[startLine-1].start
	end := spans[endLine-1].end
	oldRange := content[start:end]
	eol := dominantLineEnding(content)
	if a.Old == "" && lineEditRequiresAnchor(startLine, endLine, a.New) {
		return "", 0, fmt.Errorf("edit: old is required for multi-line line edits; re-read a narrow range around the target and include old as an anchor")
	}
	if a.Old != "" && !lineRangeOldMatches(oldRange, a.Old, eol) {
		return "", 0, fmt.Errorf("edit: line range old mismatch; selected lines %d-%d do not match old; re-read a narrow range around the target and retry", startLine, endLine)
	}
	replacement := normalizeReplacementLineEndings(a.New, eol)
	if replacement != "" && hasLineEnding(oldRange) && !hasLineEnding(replacement) {
		replacement += eol
	}
	return content[:start] + replacement + content[end:], 1, nil
}

// lineEditRequiresAnchor returns true when a line-range edit requires an
// "old" anchor. Multi-line replacements require an anchor to prevent stale
// line numbers from silently editing the wrong location. Single-line
// replacements that produce only one logical line do not require an anchor.
func lineEditRequiresAnchor(startLine, endLine int, replacement string) bool {
	if endLine != startLine {
		return true
	}
	return logicalLineCount(replacement) > 1
}

func logicalLineCount(s string) int {
	if s == "" {
		return 0
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	if strings.HasSuffix(s, "\n") {
		s = strings.TrimSuffix(s, "\n")
	}
	if s == "" {
		return 1
	}
	return strings.Count(s, "\n") + 1
}

// lineRangeOldMatches checks whether the selected line content matches the
// old anchor, normalizing line endings. Content ending in \n matches an old
// without trailing \n to allow flexible client input.
func lineRangeOldMatches(selected, old, eol string) bool {
	if selected == old {
		return true
	}
	normalizedOld := normalizeReplacementLineEndings(old, eol)
	if selected == normalizedOld {
		return true
	}
	return hasLineEnding(selected) && !hasLineEnding(normalizedOld) && selected == normalizedOld+eol
}

// contentLineSpans computes the byte index range [start, end) for each line in
// content. Lines are separated by \n; the final line may or may not be
// newline-terminated.
func contentLineSpans(content string) []lineSpan {
	if content == "" {
		return nil
	}
	spans := make([]lineSpan, 0, strings.Count(content, "\n")+1)
	start := 0
	for i := 0; i < len(content); i++ {
		if content[i] != '\n' {
			continue
		}
		spans = append(spans, lineSpan{start: start, end: i + 1})
		start = i + 1
	}
	if start < len(content) {
		spans = append(spans, lineSpan{start: start, end: len(content)})
	}
	return spans
}

// dominantLineEnding detects whether the content uses Windows line endings
// (\r\n) or Unix line endings (\n).
func dominantLineEnding(content string) string {
	if strings.Contains(content, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

// normalizeReplacementLineEndings converts the replacement text to match the
// dominant line ending of the target file. Inputs may use \n, \r\n, or \r;
// the output always uses eol.
func normalizeReplacementLineEndings(s, eol string) string {
	if eol == "\n" || s == "" {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.ReplaceAll(s, "\n", eol)
}

func hasLineEnding(s string) bool {
	return strings.HasSuffix(s, "\n") || strings.HasSuffix(s, "\r")
}

// editOldNotFoundError constructs a helpful error message when old text is not
// found, including hints about whitespace and suggestions for recovery.
func editOldNotFoundError(content, old string) error {
	hints := []string{
		"old must match the file exactly, including tabs, spaces, and line endings",
		"re-read a narrow range around the target and retry apply_patch with context or edit/multiedit with exact text",
		"do not use bash/sed/python to mutate files unless the dedicated edit tools cannot express the change",
	}
	if hasWhitespaceNormalizedMatch(content, old) {
		hints = append([]string{"a whitespace-normalized match exists; check tabs vs spaces or line endings"}, hints...)
	}
	return fmt.Errorf("edit: old string not found; %s", strings.Join(hints, "; "))
}

func hasWhitespaceNormalizedMatch(content, old string) bool {
	foldedOld := whitespaceFold(old)
	if foldedOld == "" {
		return false
	}
	return strings.Contains(whitespaceFold(content), foldedOld)
}

func whitespaceFold(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func (t *editTool) Preview(_ context.Context, raw json.RawMessage) (tool.Preview, error) {
	a, abs, err := t.parseAndResolve(raw)
	if err != nil {
		return tool.Preview{}, err
	}
	rel, _ := relToRoot(t.root, abs)

	before, err := readFileFn(abs)
	if err != nil {
		return tool.Preview{}, fmt.Errorf("edit: read %s: %w", rel, err)
	}
	after, _, err := applyEdit(string(before), a)
	if err != nil {
		return tool.Preview{}, err
	}
	diff := udiff.Unified(rel, rel, string(before), after)
	return tool.Preview{
		Summary: fmt.Sprintf("Edit %s", rel),
		Files: []tool.FileDiff{{
			Path:        rel,
			Kind:        tool.KindModify,
			UnifiedDiff: diff,
		}},
	}, nil
}

func (t *editTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	a, abs, err := t.parseAndResolve(raw)
	if err != nil {
		return tool.Result{}, err
	}
	rel, _ := relToRoot(t.root, abs)

	before, err := readFileFn(abs)
	if err != nil {
		return tool.Result{}, fmt.Errorf("edit: read %s: %w", rel, err)
	}
	after, count, err := applyEdit(string(before), a)
	if err != nil {
		return tool.Result{}, err
	}
	diff := udiff.Unified(rel, rel, string(before), after)
	// re-check the file just before writing to detect concurrent changes
	// between Preview and Execute.
	current, err := readFileFn(abs)
	if err != nil {
		return tool.Result{}, fmt.Errorf("edit: re-read %s: %w", rel, err)
	}
	if string(current) != string(before) {
		return tool.Result{}, fmt.Errorf("edit: %s changed on disk since preview", rel)
	}
	if err := os.WriteFile(abs, []byte(after), 0o644); err != nil {
		return tool.Result{}, fmt.Errorf("edit: write %s: %w", rel, err)
	}
	notifySuffix := notifyChanged(ctx, t.notifier, abs)
	return tool.Result{
		Content: fmt.Sprintf("edited %s (%d replacement(s))%s", rel, count, notifySuffix),
		Files: []tool.FileChange{{
			Path:        rel,
			Kind:        tool.KindModify,
			UnifiedDiff: diff,
		}},
	}, nil
}

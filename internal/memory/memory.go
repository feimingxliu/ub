// Package memory persists durable, agent-visible facts about the user's
// workspace ("build command is X", "issue #42 root cause is Y") and the
// user's broader environment ("prefer pnpm over npm", "VPN URL is Z").
//
// Three layers:
//
//   - global instructions: <XDG_CONFIG_HOME>/ub/instructions.md —
//     hand-written user preferences that travel with the user.
//   - project instructions: <workspace>/AGENTS.md — hand-written project
//     facts that travel with the project. (Read-only for this package;
//     loaded by the prompt harness.)
//   - auto memory: <XDG_STATE_HOME>/ub/memory/<project-key>/memory.md —
//     machine-appended facts keyed by project, never committed to git.
//
// Auto memory entries are organized by category (preference, project,
// pattern, decision, debug, general) within a single markdown file. The
// file format uses H2 section headings for categories and bullet items for
// entries, so users can hand-edit without breaking the runtime.
package memory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/feimingxliu/ub/internal/paths"
)

// Scope selects which memory source an operation targets.
type Scope string

const (
	ScopeAuto      Scope = "auto"      // project auto memory (default)
	ScopeGlobal    Scope = "global"    // global hand-written instructions
	ScopeWorkspace Scope = "workspace" // deprecated alias for auto; kept for API compat
)

// Category classifies a memory entry for prioritized injection.
type Category string

const (
	CatPreference Category = "preference" // user preferences — always injected
	CatProject    Category = "project"    // project facts — high priority
	CatPattern    Category = "pattern"    // code style/patterns — medium
	CatDecision   Category = "decision"   // architecture decisions — medium
	CatDebug      Category = "debug"      // debug notes — low, decays
	CatGeneral    Category = "general"    // misc — low
)

// categoryPriority defines injection order: lower number = higher priority.
var categoryPriority = map[Category]int{
	CatPreference: 0,
	CatProject:    1,
	CatPattern:    2,
	CatDecision:   3,
	CatGeneral:    4,
	CatDebug:      5,
}

// DefaultCategory is used when no category is specified.
const DefaultCategory = CatGeneral

// nowFunc is overridden in tests for deterministic entry timestamps.
var nowFunc = func() time.Time { return time.Now().UTC() }

// DefaultReadMaxChars is the default budget used when agent.Options does
// not specify one.
const DefaultReadMaxChars = 4000

// ValidScope reports whether s is one of the supported scope values.
func ValidScope(s string) bool {
	switch Scope(s) {
	case ScopeAuto, ScopeGlobal, ScopeWorkspace:
		return true
	}
	return false
}

// ValidCategory reports whether c is a recognized category.
func ValidCategory(c string) bool {
	_, ok := categoryPriority[Category(c)]
	return ok
}

// normalizeScope maps deprecated "workspace" → "auto" and returns the
// effective scope.
func normalizeScope(s Scope) Scope {
	if s == ScopeWorkspace {
		return ScopeAuto
	}
	return s
}

// Entry is one structured memory record.
type Entry struct {
	Category  Category
	Timestamp time.Time
	Text      string
}

// AppendAction describes how an auto-memory write changed the memory file.
type AppendAction string

const (
	AppendActionCreated AppendAction = "created"
	AppendActionMerged  AppendAction = "merged"
)

const (
	debugEntryMaxAge   = 14 * 24 * time.Hour
	generalEntryMaxAge = 180 * 24 * time.Hour
	maxAutoEntries     = 200
)

// AppendOutcome reports the durable effect of one memory write.
type AppendOutcome struct {
	Path            string
	Heading         string
	Scope           Scope
	Category        Category
	Text            string
	Action          AppendAction
	DroppedExpired  int
	DroppedOverflow int
}

// Path returns the absolute path of the memory file for one scope.
//
//   - ScopeAuto: $XDG_STATE_HOME/ub/memory/<project-key>/memory.md
//   - ScopeGlobal: $XDG_CONFIG_HOME/ub/instructions.md
//
// workspaceRoot MUST be non-empty for ScopeAuto.
func Path(workspaceRoot string, scope Scope) (string, error) {
	scope = normalizeScope(scope)
	switch scope {
	case ScopeAuto:
		if strings.TrimSpace(workspaceRoot) == "" {
			return "", errors.New("memory: workspace root required for auto scope")
		}
		key, err := paths.ProjectKey(workspaceRoot)
		if err != nil {
			return "", fmt.Errorf("memory: project key: %w", err)
		}
		stateRoot, err := paths.StateRoot()
		if err != nil {
			return "", err
		}
		return filepath.Join(stateRoot, "memory", key, "memory.md"), nil
	case ScopeGlobal:
		cfgHome, err := paths.ConfigHome()
		if err != nil {
			return "", err
		}
		return filepath.Join(cfgHome, "ub", "instructions.md"), nil
	default:
		return "", fmt.Errorf("memory: unknown scope %q", scope)
	}
}

// Append writes a new entry to the scope's memory file. Auto-memory entries
// are de-duplicated within the same category: similar existing text is
// updated (timestamp refreshed, text replaced) instead of appending a
// duplicate. Global instructions are append-only so hand-written content is
// never rewritten by the structured auto-memory parser.
//
// Returns the absolute path of the file and a human-readable heading string.
func Append(workspaceRoot string, scope Scope, category Category, text string) (string, string, error) {
	out, err := AppendWithOutcome(workspaceRoot, scope, category, text)
	if err != nil {
		return "", "", err
	}
	return out.Path, out.Heading, nil
}

// AppendWithOutcome writes a new entry and returns structured metadata for
// audit events. It applies privacy guardrails, conflict merge, and decay to
// machine-managed auto memory while keeping global instructions append-only.
func AppendWithOutcome(workspaceRoot string, scope Scope, category Category, text string) (AppendOutcome, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return AppendOutcome{}, errors.New("memory: text is required")
	}
	if _, ok := categoryPriority[category]; !ok {
		return AppendOutcome{}, fmt.Errorf("memory: invalid category %q", category)
	}
	if reason := privacyRejectReason(text); reason != "" {
		return AppendOutcome{}, fmt.Errorf("memory: privacy guard rejected entry: %s", reason)
	}
	scope = normalizeScope(scope)
	path, err := Path(workspaceRoot, scope)
	if err != nil {
		return AppendOutcome{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return AppendOutcome{}, fmt.Errorf("memory: mkdir: %w", err)
	}

	ts := nowFunc()
	heading := fmt.Sprintf("## %s", ts.Format(time.RFC3339))
	out := AppendOutcome{
		Path:     path,
		Heading:  heading,
		Scope:    scope,
		Category: category,
		Text:     text,
		Action:   AppendActionCreated,
	}

	newEntry := Entry{Category: category, Timestamp: ts, Text: text}
	if scope == ScopeGlobal {
		if err := appendGlobalInstruction(path, newEntry); err != nil {
			return AppendOutcome{}, fmt.Errorf("memory: write: %w", err)
		}
		return out, nil
	}

	// Load existing auto-memory entries for dedup check.
	entries, err := parseFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return AppendOutcome{}, fmt.Errorf("memory: read: %w", err)
	}

	deduped := false
	for i, e := range entries {
		if e.Category == category && shouldMerge(e.Text, text) {
			entries[i] = newEntry
			deduped = true
			out.Action = AppendActionMerged
			break
		}
	}
	if !deduped {
		entries = append(entries, newEntry)
	}
	entries, out.DroppedExpired, out.DroppedOverflow = applyDecay(entries, ts)

	if err := writeFile(path, entries); err != nil {
		return AppendOutcome{}, fmt.Errorf("memory: write: %w", err)
	}
	return out, nil
}

func appendGlobalInstruction(path string, e Entry) error {
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	prefix := ""
	if strings.TrimSpace(string(existing)) != "" {
		if len(existing) > 0 && existing[len(existing)-1] == '\n' {
			prefix = "\n"
		} else {
			prefix = "\n\n"
		}
	}
	entry := fmt.Sprintf("%s## %s\n\n- [%s] %s\n", prefix, e.Timestamp.Format(time.RFC3339), e.Category, e.Text)

	// Append-only so hand-written instructions are never discarded by the
	// structured auto-memory parser.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(entry); err != nil {
		return err
	}
	return nil
}

// Read returns the concatenated memory the agent should see this turn:
// global instructions first, then auto memory entries, with section
// markers so the model can tell them apart.
//
// When maxChars > 0 and the combined text exceeds the budget, auto memory
// entries are included by category priority (preference first, debug last).
// Within the same category, newer entries are preferred. Global instructions
// are always kept. A "... [memory truncated]" marker is prepended when some
// entries are dropped.
func Read(workspaceRoot string, maxChars int) string {
	var parts []string
	var globalBody string

	// Global instructions (hand-written, always included).
	if gp, err := Path("", ScopeGlobal); err == nil {
		if body := readFile(gp); body != "" {
			globalBody = "<!-- global instructions -->\n" + body
			parts = append(parts, globalBody)
		}
	}

	// Auto memory (machine-appended, budgeted).
	var autoEntries []Entry
	if strings.TrimSpace(workspaceRoot) != "" {
		if ap, err := Path(workspaceRoot, ScopeAuto); err == nil {
			entries, _ := parseFile(ap)
			autoEntries = entries
		}
	}

	if len(parts) == 0 && len(autoEntries) == 0 {
		return ""
	}

	// If no budget or everything fits, just concatenate.
	autoRendered := renderAutoMemory(autoEntries)
	if len(parts) > 0 {
		parts = append(parts, "<!-- auto memory -->\n"+autoRendered)
	} else {
		parts = append(parts, "<!-- auto memory -->\n"+autoRendered)
	}
	// Remove empty auto memory section.
	filtered := parts[:0]
	for _, p := range parts {
		if strings.TrimSpace(strings.TrimPrefix(p, "<!-- auto memory -->")) != "" ||
			!strings.HasPrefix(p, "<!-- auto memory -->") {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		return ""
	}

	joined := strings.Join(filtered, "\n---\n")
	if maxChars <= 0 || len(joined) <= maxChars {
		return joined
	}

	// Budget exceeded: rebuild auto memory with priority-based selection.
	truncMarker := "... [memory truncated]\n"
	budget := maxChars - len(truncMarker)
	if budget < 0 {
		budget = 0
	}

	// Reserve space for global instructions.
	globalBudget := 0
	if globalBody != "" {
		globalBudget = len(globalBody) + len("\n---\n")
		if globalBudget > budget {
			globalBudget = budget
		}
	}
	autoBudget := budget - globalBudget

	// Sort auto entries by priority then recency.
	sorted := make([]Entry, len(autoEntries))
	copy(sorted, autoEntries)
	sort.Slice(sorted, func(i, j int) bool {
		pi, pj := categoryPriority[sorted[i].Category], categoryPriority[sorted[j].Category]
		if pi != pj {
			return pi < pj
		}
		return sorted[i].Timestamp.After(sorted[j].Timestamp)
	})

	var autoB strings.Builder
	autoB.WriteString("<!-- auto memory -->\n")
	autoHeaderLen := autoB.Len()
	for _, e := range sorted {
		line := fmt.Sprintf("[%s] %s\n", e.Category, e.Text)
		if autoB.Len()-autoHeaderLen+len(line) > autoBudget {
			continue
		}
		autoB.WriteString(line)
	}

	var result strings.Builder
	result.WriteString(truncMarker)
	if globalBody != "" {
		result.WriteString(globalBody)
		result.WriteString("\n---\n")
	}
	result.WriteString(autoB.String())
	return result.String()
}

// renderAutoMemory renders entries as category-sectioned markdown.
func renderAutoMemory(entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}

	groups := make(map[Category][]Entry)
	for _, e := range entries {
		groups[e.Category] = append(groups[e.Category], e)
	}

	var cats []Category
	for c := range groups {
		cats = append(cats, c)
	}
	sort.Slice(cats, func(i, j int) bool {
		return categoryPriority[cats[i]] < categoryPriority[cats[j]]
	})

	var b strings.Builder
	for _, cat := range cats {
		b.WriteString("## " + string(cat) + "\n\n")
		items := groups[cat]
		sort.Slice(items, func(i, j int) bool {
			return items[i].Timestamp.After(items[j].Timestamp)
		})
		for _, e := range items {
			fmt.Fprintf(&b, "- %s *(%s)*\n", e.Text, e.Timestamp.Format(time.RFC3339))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// Recall searches auto memory entries matching a query. If query is empty,
// all entries are returned (optionally filtered by category). Returns an
// empty slice when no memory file exists.
func Recall(workspaceRoot string, query string, category Category) ([]Entry, error) {
	if strings.TrimSpace(workspaceRoot) == "" {
		return nil, errors.New("memory: workspace root required for recall")
	}
	path, err := Path(workspaceRoot, ScopeAuto)
	if err != nil {
		return nil, err
	}
	entries, err := parseFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var result []Entry
	for _, e := range entries {
		if category != "" && e.Category != category {
			continue
		}
		if query != "" && !strings.Contains(
			strings.ToLower(e.Text),
			strings.ToLower(query),
		) {
			continue
		}
		result = append(result, e)
	}
	return result, nil
}

// --- File format ---

// The auto memory file uses category-sectioned markdown:
//
//	## preference
//
//	- prefer pnpm over npm *(2026-05-27T10:00:00Z)*
//	- always use conventional commits *(2026-05-28T09:00:00Z)*
//
//	## project
//
//	- build is `make build` *(2026-05-27T10:00:00Z)*
//
// Entries are grouped by category. Each entry is a bullet point with an
// italic timestamp. The file is fully hand-editable.

// writeFile serializes entries to category-sectioned markdown.
func writeFile(path string, entries []Entry) error {
	// Group by category, preserving category priority order.
	groups := make(map[Category][]Entry)
	for _, e := range entries {
		groups[e.Category] = append(groups[e.Category], e)
	}

	var cats []Category
	for c := range groups {
		cats = append(cats, c)
	}
	sort.Slice(cats, func(i, j int) bool {
		return categoryPriority[cats[i]] < categoryPriority[cats[j]]
	})

	var b strings.Builder
	for _, cat := range cats {
		b.WriteString("## " + string(cat) + "\n\n")
		items := groups[cat]
		// Sort within category: newest first.
		sort.Slice(items, func(i, j int) bool {
			return items[i].Timestamp.After(items[j].Timestamp)
		})
		for _, e := range items {
			fmt.Fprintf(&b, "- %s *(%s)*\n", e.Text, e.Timestamp.Format(time.RFC3339))
		}
		b.WriteString("\n")
	}

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// parseFile reads and parses a category-sectioned markdown file.
func parseFile(path string) ([]Entry, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseEntries(string(body)), nil
}

// parseEntries parses category-sectioned markdown into entries.
func parseEntries(content string) []Entry {
	var entries []Entry
	var curCat Category

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			catStr := strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			if ValidCategory(catStr) {
				curCat = Category(catStr)
			} else {
				curCat = CatGeneral
			}
			continue
		}
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		text, ts, ok := parseBullet(trimmed[2:])
		if !ok {
			continue
		}
		entries = append(entries, Entry{
			Category:  curCat,
			Timestamp: ts,
			Text:      text,
		})
	}
	return entries
}

// parseBullet parses "text *(timestamp)*" from a bullet item.
func parseBullet(s string) (text string, ts time.Time, ok bool) {
	// Look for trailing *(RFC3339)*
	idx := strings.LastIndex(s, "*(")
	if idx < 0 {
		return s, time.Time{}, false
	}
	text = strings.TrimSpace(s[:idx])
	rest := s[idx+2:]
	closeIdx := strings.Index(rest, ")*")
	if closeIdx < 0 {
		return s, time.Time{}, false
	}
	tsStr := rest[:closeIdx]
	parsed, err := time.Parse(time.RFC3339, tsStr)
	if err != nil {
		return s, time.Time{}, false
	}
	return text, parsed, true
}

// readFile reads a plain file and returns its trimmed content, or empty
// string on any error.
func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// isSimilar reports whether two texts are similar enough to be considered
// duplicates. Current heuristic: normalized substring containment, or
// sufficient word overlap.
func isSimilar(a, b string) bool {
	al := normalize(a)
	bl := normalize(b)
	if al == "" || bl == "" {
		return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
	}
	// Substring containment after normalization.
	if strings.Contains(al, bl) || strings.Contains(bl, al) {
		return true
	}
	// Word overlap: if ≥60% of words in the shorter string appear in the
	// longer one, consider similar.
	wordsA := wordSet(al)
	wordsB := wordSet(bl)
	if len(wordsA) == 0 || len(wordsB) == 0 {
		return false
	}
	shorter, longer := wordsA, wordsB
	if len(wordsA) > len(wordsB) {
		shorter, longer = wordsB, wordsA
	}
	common := 0
	for w := range shorter {
		if longer[w] {
			common++
		}
	}
	return float64(common)/float64(len(shorter)) >= 0.6
}

func shouldMerge(existing, incoming string) bool {
	if isSimilar(existing, incoming) {
		return true
	}
	existingTopic := topicKey(existing)
	incomingTopic := topicKey(incoming)
	return existingTopic != "" && existingTopic == incomingTopic
}

func topicKey(text string) string {
	words := wordSet(normalize(text))
	hasAny := func(values ...string) bool {
		for _, value := range values {
			if words[value] {
				return true
			}
		}
		return false
	}
	switch {
	case hasAny("build", "compile"):
		return "project:build"
	case hasAny("test", "tests", "testing", "validation", "verify"):
		return "project:test"
	case hasAny("lint", "linter", "staticcheck", "vet"):
		return "project:lint"
	case hasAny("format", "formatter", "gofmt", "prettier"):
		return "project:format"
	case hasAny("commit", "commits", "conventional"):
		return "project:commit"
	case hasAny("roadmap", "requirements", "design", "docs"):
		return "project:docs"
	}
	return ""
}

func applyDecay(entries []Entry, now time.Time) ([]Entry, int, int) {
	if len(entries) == 0 {
		return nil, 0, 0
	}
	kept := entries[:0]
	expired := 0
	for _, e := range entries {
		if entryExpired(e, now) {
			expired++
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) <= maxAutoEntries {
		return kept, expired, 0
	}
	sort.SliceStable(kept, func(i, j int) bool {
		pi, pj := categoryPriority[kept[i].Category], categoryPriority[kept[j].Category]
		if pi != pj {
			return pi < pj
		}
		return kept[i].Timestamp.After(kept[j].Timestamp)
	})
	overflow := len(kept) - maxAutoEntries
	kept = kept[:maxAutoEntries]
	return kept, expired, overflow
}

func entryExpired(e Entry, now time.Time) bool {
	if e.Timestamp.IsZero() || now.IsZero() || e.Timestamp.After(now) {
		return false
	}
	age := now.Sub(e.Timestamp)
	switch e.Category {
	case CatDebug:
		return age > debugEntryMaxAge
	case CatGeneral:
		return age > generalEntryMaxAge
	default:
		return false
	}
}

func privacyRejectReason(text string) string {
	lower := strings.ToLower(text)
	for _, marker := range []string{
		"authorization:",
		"bearer ",
		"api_key",
		"api key",
		"access token",
		"secret key",
		"client_secret",
		"private key",
		"password=",
		"passwd=",
		"token=",
	} {
		if strings.Contains(lower, marker) {
			return "looks like credential material"
		}
	}
	for _, marker := range []string{"temporary debug", "temp debug", "stack trace", "stacktrace"} {
		if strings.Contains(lower, marker) {
			return "looks like temporary debug state"
		}
	}
	for _, field := range strings.Fields(text) {
		if looksLikeSecret(field) {
			return "contains a high-entropy token-like value"
		}
	}
	return ""
}

func looksLikeSecret(value string) bool {
	value = strings.Trim(value, "`'\"()[]{}<>,.;:")
	if len(value) < 32 || len(value) > 256 {
		return false
	}
	alphaNum := 0
	classes := 0
	hasLower, hasUpper, hasDigit, hasSymbol := false, false, false, false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			alphaNum++
			hasLower = true
		case r >= 'A' && r <= 'Z':
			alphaNum++
			hasUpper = true
		case r >= '0' && r <= '9':
			alphaNum++
			hasDigit = true
		case r == '_' || r == '-' || r == '.' || r == '/':
			hasSymbol = true
		default:
			return false
		}
	}
	for _, ok := range []bool{hasLower, hasUpper, hasDigit, hasSymbol} {
		if ok {
			classes++
		}
	}
	return alphaNum >= 24 && classes >= 3
}

// normalize strips punctuation and collapses whitespace for comparison.
func normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func wordSet(s string) map[string]bool {
	words := strings.Fields(s)
	set := make(map[string]bool, len(words))
	for _, w := range words {
		set[w] = true
	}
	return set
}

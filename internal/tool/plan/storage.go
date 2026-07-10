package plan

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/feimingxliu/ub/internal/workspace/paths"
)

const (
	plansDirPerm      = 0o755
	planFilePerm      = 0o644
	planTimestampForm = "20060102T150405Z"
	slugMaxLen        = 40

	statusInProgress = "in_progress"
	statusComplete   = "complete"
)

// nowFunc is overridden in tests for deterministic plan ids and log lines.
var nowFunc = func() time.Time { return time.Now().UTC() }

// planRoot returns the absolute path of the plans directory for a workspace.
// Plans are stored under $XDG_STATE_HOME/ub/plans/<project-key>/.
func planRoot(workspace string) (string, error) {
	key, err := paths.ProjectKey(workspace)
	if err != nil {
		return "", fmt.Errorf("plan: project key: %w", err)
	}
	stateRoot, err := paths.StateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(stateRoot, "plans", key), nil
}

// planPath returns the absolute path of one plan markdown file.
func planPath(workspace, planID string) (string, error) {
	root, err := planRoot(workspace)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, planID+".md"), nil
}

// Path returns the absolute path of one plan markdown file for UI review
// flows. It does not create the file.
func Path(workspace, planID string) (string, error) {
	return planPath(workspace, planID)
}

// Info describes one persisted plan artifact for UI selection.
type Info struct {
	ID        string
	Title     string
	Status    string
	Path      string
	StepCount int
	UpdatedAt time.Time
}

// List returns persisted plan artifacts for a workspace, newest first.
func List(workspace string) ([]Info, error) {
	root, err := planRoot(workspace)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var plans []Info
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		path := filepath.Join(root, entry.Name())
		stat, err := entry.Info()
		if err != nil {
			return nil, err
		}
		id := strings.TrimSuffix(entry.Name(), ".md")
		info := Info{
			ID:        id,
			Title:     id,
			Path:      path,
			UpdatedAt: stat.ModTime(),
		}
		if doc, err := loadPlan(path); err == nil {
			info.Title = doc.title
			info.Status = doc.status
			info.StepCount = len(doc.steps)
		}
		plans = append(plans, info)
	}
	sort.SliceStable(plans, func(i, j int) bool {
		if !plans[i].UpdatedAt.Equal(plans[j].UpdatedAt) {
			return plans[i].UpdatedAt.After(plans[j].UpdatedAt)
		}
		return plans[i].ID > plans[j].ID
	})
	return plans, nil
}

var slugReplacer = regexp.MustCompile(`[^A-Za-z0-9-]+`)

// slugify produces a filesystem-safe, human-readable suffix for a plan id.
func slugify(title string) string {
	s := slugReplacer.ReplaceAllString(title, "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if s == "" {
		s = "plan"
	}
	if len(s) > slugMaxLen {
		s = strings.TrimRight(s[:slugMaxLen], "-")
	}
	return strings.ToLower(s)
}

// newPlanID returns a fresh plan id from a title. Two calls within the same
// second for the same title would collide; plan_write rejects on collision so
// the model can retry with a more specific title.
func newPlanID(title string) string {
	return nowFunc().Format(planTimestampForm) + "-" + slugify(title)
}

const (
	stepMarkerPending    = " "
	stepMarkerInProgress = ">"
	stepMarkerDone       = "x"
	stepMarkerSkipped    = "~"
	stepMarkerFailed     = "!"
)

// step is one entry in a plan's Steps section.
type step struct {
	marker string // " ", ">", "x", "~", "!"
	index  int
	text   string
}

// planDoc is the parsed in-memory form of a plan markdown file.
type planDoc struct {
	title   string
	created string
	status  string
	steps   []step
	notes   string
	log     []string
}

// statusMarker translates a status string from plan_update_step into the
// checkbox character that goes inside `- [ ]`.
func statusMarker(status string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case statusInProgress:
		return stepMarkerInProgress, nil
	case "done":
		return stepMarkerDone, nil
	case "skipped":
		return stepMarkerSkipped, nil
	case "failed":
		return stepMarkerFailed, nil
	case "pending":
		return stepMarkerPending, nil
	default:
		return "", fmt.Errorf("invalid status %q (want in_progress|done|skipped|failed|pending)", status)
	}
}

func renderPlan(p planDoc) string {
	var b strings.Builder
	b.WriteString("# " + p.title + "\n\n")
	b.WriteString("Created: " + p.created + "\n")
	b.WriteString("Status: " + p.status + "\n\n")
	b.WriteString("## Steps\n\n")
	for _, s := range p.steps {
		fmt.Fprintf(&b, "- [%s] %d. %s\n", s.marker, s.index, s.text)
	}
	b.WriteString("\n## Notes\n\n")
	if strings.TrimSpace(p.notes) != "" {
		b.WriteString(strings.TrimRight(p.notes, "\n") + "\n")
	}
	b.WriteString("\n## Log\n\n")
	for _, line := range p.log {
		b.WriteString(line + "\n")
	}
	return b.String()
}

// loadPlan reads and parses an existing plan file. It is intentionally
// conservative: it expects exactly the structure renderPlan produces, so
// hand-edits that diverge will fail the parser instead of being silently
// corrupted on next save.
func loadPlan(path string) (planDoc, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return planDoc{}, fmt.Errorf("plan not found: %s", filepath.Base(path))
		}
		return planDoc{}, err
	}
	defer f.Close()

	var p planDoc
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	section := ""
	var notesBuf, logBuf []string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "# "):
			p.title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			section = "header"
		case strings.HasPrefix(line, "Created: "):
			p.created = strings.TrimSpace(strings.TrimPrefix(line, "Created: "))
		case strings.HasPrefix(line, "Status: "):
			p.status = strings.TrimSpace(strings.TrimPrefix(line, "Status: "))
		case line == "## Steps":
			section = "steps"
		case line == "## Notes":
			section = "notes"
		case line == "## Log":
			section = "log"
		default:
			switch section {
			case "steps":
				if s, ok := parseStepLine(line); ok {
					p.steps = append(p.steps, s)
				}
			case "notes":
				notesBuf = append(notesBuf, line)
			case "log":
				if strings.TrimSpace(line) != "" {
					logBuf = append(logBuf, line)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return planDoc{}, err
	}
	if p.title == "" {
		return planDoc{}, fmt.Errorf("plan file missing title heading")
	}
	if len(p.steps) == 0 {
		return planDoc{}, fmt.Errorf("plan file has no steps")
	}
	p.notes = strings.TrimSpace(strings.Join(notesBuf, "\n"))
	p.log = logBuf
	return p, nil
}

var stepLineRE = regexp.MustCompile(`^- \[(.)\] (\d+)\. (.*)$`)

func parseStepLine(line string) (step, bool) {
	m := stepLineRE.FindStringSubmatch(line)
	if m == nil {
		return step{}, false
	}
	var idx int
	if _, err := fmt.Sscanf(m[2], "%d", &idx); err != nil {
		return step{}, false
	}
	return step{marker: m[1], index: idx, text: m[3]}, true
}

// savePlan atomically writes p to path via temp file + rename.
func savePlan(path string, p planDoc) error {
	if err := os.MkdirAll(filepath.Dir(path), plansDirPerm); err != nil {
		return fmt.Errorf("create plans dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".plan-*.tmp")
	if err != nil {
		return fmt.Errorf("create plan tmp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.WriteString(renderPlan(p)); err != nil {
		tmp.Close()
		return fmt.Errorf("write plan tmp: %w", err)
	}
	if err := tmp.Chmod(planFilePerm); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod plan tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close plan tmp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename plan: %w", err)
	}
	tmpName = ""
	return nil
}

// allStepsFinished reports whether every step has a terminal marker, i.e. the
// plan should transition to Status: complete.
func allStepsFinished(steps []step) bool {
	for _, s := range steps {
		if s.marker == stepMarkerPending || s.marker == stepMarkerInProgress {
			return false
		}
	}
	return true
}

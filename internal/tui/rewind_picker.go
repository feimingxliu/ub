package tui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/feimingxliu/ub/internal/tui/tuitheme"
)

type rewindPickerPhase string

const (
	rewindPickerTargets rewindPickerPhase = "targets"
	rewindPickerMode    rewindPickerPhase = "mode"
)

type rewindPicker struct {
	all     []RewindTarget
	targets []RewindTarget
	query   string
	index   int
	phase   rewindPickerPhase
	chosen  RewindTarget
	modeIdx int
}

type rewindModeOption struct {
	label       string
	description string
	revertFiles bool
}

var rewindModeOptions = []rewindModeOption{
	{label: "conversation only", description: "keep workspace files as-is", revertFiles: false},
	{label: "conversation + files", description: "restore checkpointed workspace files", revertFiles: true},
}

func newRewindPicker(targets []RewindTarget) *rewindPicker {
	picker := &rewindPicker{
		all:   append([]RewindTarget(nil), targets...),
		phase: rewindPickerTargets,
	}
	picker.refilter()
	return picker
}

func (p *rewindPicker) selectedTarget() RewindTarget {
	if p == nil || len(p.targets) == 0 {
		return RewindTarget{}
	}
	return p.targets[p.index]
}

func (p *rewindPicker) selectedMode() rewindModeOption {
	if p == nil || len(rewindModeOptions) == 0 {
		return rewindModeOption{}
	}
	if p.modeIdx < 0 || p.modeIdx >= len(rewindModeOptions) {
		return rewindModeOptions[0]
	}
	return rewindModeOptions[p.modeIdx]
}

func (p *rewindPicker) chooseTarget(target RewindTarget) {
	if p == nil {
		return
	}
	p.chosen = target
	p.phase = rewindPickerMode
	p.modeIdx = 0
}

func (p *rewindPicker) next() {
	if p == nil {
		return
	}
	if p.phase == rewindPickerMode {
		p.modeIdx = (p.modeIdx + 1) % len(rewindModeOptions)
		return
	}
	if len(p.targets) == 0 {
		return
	}
	p.index = (p.index + 1) % len(p.targets)
}

func (p *rewindPicker) previous() {
	if p == nil {
		return
	}
	if p.phase == rewindPickerMode {
		p.modeIdx = (p.modeIdx + len(rewindModeOptions) - 1) % len(rewindModeOptions)
		return
	}
	if len(p.targets) == 0 {
		return
	}
	p.index = (p.index + len(p.targets) - 1) % len(p.targets)
}

func (p *rewindPicker) appendRune(r rune) {
	if p == nil || p.phase != rewindPickerTargets || !unicode.IsPrint(r) {
		return
	}
	p.query += string(r)
	p.refilter()
}

func (p *rewindPicker) backspace() {
	if p == nil || p.phase != rewindPickerTargets || p.query == "" {
		return
	}
	runes := []rune(p.query)
	p.query = string(runes[:len(runes)-1])
	p.refilter()
}

func (p *rewindPicker) clearQuery() {
	if p == nil || p.phase != rewindPickerTargets || p.query == "" {
		return
	}
	p.query = ""
	p.refilter()
}

func (p *rewindPicker) refilter() {
	if p == nil {
		return
	}
	p.targets = nil
	query := strings.TrimSpace(p.query)
	for _, target := range p.all {
		if query == "" || rewindTargetMatchesQuery(target, query) {
			p.targets = append(p.targets, target)
		}
	}
	p.index = 0
	if len(p.targets) > 0 && p.index >= len(p.targets) {
		p.index = len(p.targets) - 1
	}
}

func (p *rewindPicker) view(width int, styles tuitheme.Styles) string {
	if p == nil {
		return ""
	}
	if p.phase == rewindPickerMode {
		return p.modeView(width, styles)
	}
	return p.targetView(width, styles)
}

func (p *rewindPicker) targetView(width int, styles tuitheme.Styles) string {
	var b strings.Builder
	b.WriteString(styles.Render(styles.Picker.Title, truncateText("◇ rewind to before a user message (type filter, enter select, esc cancel)", width)))
	b.WriteByte('\n')
	filter := p.query
	if filter == "" {
		filter = "all"
	}
	b.WriteString(styles.Render(styles.Picker.Item, truncateText("  filter: "+filter, width)))
	if len(p.targets) == 0 {
		b.WriteByte('\n')
		b.WriteString(styles.Render(styles.Picker.Empty, truncateText("  no matching user turns", width)))
		return b.String()
	}
	for i, target := range p.targets {
		b.WriteByte('\n')
		marker := "  "
		if i == p.index {
			marker = "> "
		}
		when := "-"
		if !target.Time.IsZero() {
			when = target.Time.Local().Format("2006-01-02 15:04")
		}
		files := ""
		if len(target.AffectedFiles) > 0 {
			files = fmt.Sprintf("  %d file(s)", len(target.AffectedFiles))
		}
		line := truncateText(fmt.Sprintf("%sturn %d  %s%s  %s", marker, target.Turn, when, files, singleLine(target.Text)), width)
		if i == p.index {
			b.WriteString(styles.Render(styles.Picker.Selected, line))
			continue
		}
		b.WriteString(styles.Render(styles.Picker.Item, line))
	}
	return b.String()
}

func (p *rewindPicker) modeView(width int, styles tuitheme.Styles) string {
	var b strings.Builder
	b.WriteString(styles.Render(styles.Picker.Title, truncateText(fmt.Sprintf("◇ rewind turn %d: choose file handling", p.chosen.Turn), width)))
	b.WriteByte('\n')
	b.WriteString(styles.Render(styles.Picker.Item, truncateText("  prompt: "+singleLine(p.chosen.Text), width)))
	if len(p.chosen.AffectedFiles) > 0 {
		b.WriteByte('\n')
		b.WriteString(styles.Render(styles.Picker.Item, truncateText("  affected: "+rewindAffectedFilesText(p.chosen.AffectedFiles), width)))
	}
	for i, option := range rewindModeOptions {
		b.WriteByte('\n')
		marker := "  "
		if i == p.modeIdx {
			marker = "> "
		}
		line := truncateText(fmt.Sprintf("%s%s  %s", marker, option.label, option.description), width)
		if i == p.modeIdx {
			b.WriteString(styles.Render(styles.Picker.Selected, line))
			continue
		}
		b.WriteString(styles.Render(styles.Picker.Item, line))
	}
	return b.String()
}

func rewindTargetMatchesQuery(target RewindTarget, query string) bool {
	haystacks := []string{
		fmt.Sprintf("%d", target.Turn),
		target.Text,
	}
	if !target.Time.IsZero() {
		haystacks = append(haystacks, target.Time.Local().Format("2006-01-02 15:04"))
	}
	for _, file := range target.AffectedFiles {
		haystacks = append(haystacks, file.Path, file.Kind)
	}
	for _, value := range haystacks {
		if fuzzyMatch(value, query) {
			return true
		}
	}
	return false
}

func rewindAffectedFilesText(files []RewindFileChange) string {
	const maxFiles = 4
	parts := make([]string, 0, min(len(files), maxFiles))
	for i, file := range files {
		if i >= maxFiles {
			break
		}
		label := strings.TrimSpace(file.Path)
		if label == "" {
			label = "(unknown)"
		}
		if kind := strings.TrimSpace(file.Kind); kind != "" {
			label += " " + kind
		}
		parts = append(parts, label)
	}
	if len(files) > maxFiles {
		parts = append(parts, fmt.Sprintf("+%d more", len(files)-maxFiles))
	}
	return strings.Join(parts, ", ")
}

func singleLine(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

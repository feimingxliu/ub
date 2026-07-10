package tui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/feimingxliu/ub/internal/tui/theme"
)

const maxFileMentionCandidates = 50

type filePicker struct {
	files []string
	index int
	query string
	err   string
}

func newFilePicker(files []string, query string, err error) *filePicker {
	picker := &filePicker{
		files: append([]string(nil), files...),
		query: query,
	}
	if err != nil {
		picker.err = err.Error()
	}
	return picker
}

func (p *filePicker) selected() string {
	if p == nil || len(p.files) == 0 {
		return ""
	}
	return p.files[selectedIndex(p.index, len(p.files))]
}

func (p *filePicker) next() {
	if p == nil || len(p.files) == 0 {
		return
	}
	p.index = nextPickerIndex(p.index, len(p.files))
}

func (p *filePicker) previous() {
	if p == nil || len(p.files) == 0 {
		return
	}
	p.index = previousPickerIndex(p.index, len(p.files))
}

func (p *filePicker) view(width int, styles tuitheme.Styles) string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	title := "◇ attach file (enter/tab insert, esc cancel)"
	if p.query != "" {
		title = fmt.Sprintf("◇ attach file @%s (enter/tab insert, esc cancel)", p.query)
	}
	b.WriteString(renderPickerTitle(styles, width, title))
	if p.err != "" {
		b.WriteByte('\n')
		b.WriteString(renderPickerEmpty(styles, width, "  "+p.err))
		return b.String()
	}
	if len(p.files) == 0 {
		b.WriteByte('\n')
		b.WriteString(renderPickerEmpty(styles, width, "  no matching files"))
		return b.String()
	}
	for i, path := range p.files {
		b.WriteByte('\n')
		selected := i == selectedIndex(p.index, len(p.files))
		b.WriteString(renderPickerChoiceLine(styles, width, selected, path))
	}
	return b.String()
}

type fileMentionToken struct {
	start  int
	end    int
	prefix string
}

// activeFileMention detects a pending @mention on a single logical line at the
// given column (rune index within that line). Multiline input is handled by
// the caller, which passes only the cursor's current line.
func activeFileMention(line string, col int) (fileMentionToken, bool) {
	runes := []rune(line)
	col = clampInt(col, 0, len(runes))
	tokenStart := col
	for tokenStart > 0 && !unicode.IsSpace(runes[tokenStart-1]) {
		tokenStart--
	}
	at := -1
	for i := tokenStart; i < col; i++ {
		if runes[i] == '@' {
			at = i
		}
	}
	if at < 0 {
		return fileMentionToken{}, false
	}
	return fileMentionToken{
		start:  at,
		end:    col,
		prefix: string(runes[at+1 : col]),
	}, true
}

// insertFileMention replaces the mention token on a single logical line with
// the quoted @path reference and returns the new line plus the column (rune
// index within that line) where the cursor should land.
func insertFileMention(line string, token fileMentionToken, path string) (string, int) {
	runes := []rune(line)
	token.start = clampInt(token.start, 0, len(runes))
	token.end = clampInt(token.end, token.start, len(runes))
	ref := "@" + quoteFileMentionPath(path) + " "
	next := string(runes[:token.start]) + ref + string(runes[token.end:])
	return next, token.start + len([]rune(ref))
}

func quoteFileMentionPath(path string) string {
	if strings.ContainsAny(path, " \t\r\n\"'") {
		return strconv.Quote(path)
	}
	return path
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

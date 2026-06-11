package tui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/feimingxliu/ub/internal/app/ub/tui/tuitheme"
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
	p.index = (selectedIndex(p.index, len(p.files)) + 1) % len(p.files)
}

func (p *filePicker) previous() {
	if p == nil || len(p.files) == 0 {
		return
	}
	p.index = (selectedIndex(p.index, len(p.files)) + len(p.files) - 1) % len(p.files)
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
	b.WriteString(styles.Render(styles.Picker.Title, truncateText(title, width)))
	if p.err != "" {
		b.WriteByte('\n')
		b.WriteString(styles.Render(styles.Picker.Empty, truncateText("  "+p.err, width)))
		return b.String()
	}
	if len(p.files) == 0 {
		b.WriteByte('\n')
		b.WriteString(styles.Render(styles.Picker.Empty, truncateText("  no matching files", width)))
		return b.String()
	}
	for i, path := range p.files {
		b.WriteByte('\n')
		marker := "  "
		if i == selectedIndex(p.index, len(p.files)) {
			marker = "> "
		}
		line := truncateText(marker+path, width)
		if i == selectedIndex(p.index, len(p.files)) {
			b.WriteString(styles.Render(styles.Picker.Selected, line))
			continue
		}
		b.WriteString(styles.Render(styles.Picker.Item, line))
	}
	return b.String()
}

type fileMentionToken struct {
	start  int
	end    int
	prefix string
}

func activeFileMention(value string, cursor int) (fileMentionToken, bool) {
	runes := []rune(value)
	cursor = clampInt(cursor, 0, len(runes))
	tokenStart := cursor
	for tokenStart > 0 && !unicode.IsSpace(runes[tokenStart-1]) {
		tokenStart--
	}
	at := -1
	for i := tokenStart; i < cursor; i++ {
		if runes[i] == '@' {
			at = i
		}
	}
	if at < 0 {
		return fileMentionToken{}, false
	}
	return fileMentionToken{
		start:  at,
		end:    cursor,
		prefix: string(runes[at+1 : cursor]),
	}, true
}

func insertFileMention(value string, token fileMentionToken, path string) (string, int) {
	runes := []rune(value)
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

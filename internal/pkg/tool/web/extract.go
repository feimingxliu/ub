package web

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

func extractText(data []byte, contentType string) (string, string) {
	switch {
	case strings.Contains(contentType, "html") || looksLikeHTML(data):
		return extractHTMLText(data), "html"
	case strings.Contains(contentType, "pdf") || bytes.HasPrefix(bytes.TrimSpace(data), []byte("%PDF")):
		return extractPDFText(data), "pdf"
	default:
		if utf8.Valid(data) {
			return string(data), "text"
		}
		return string(bytes.ToValidUTF8(data, []byte(" "))), "text"
	}
}

func looksLikeHTML(data []byte) bool {
	prefix := strings.ToLower(string(bytes.TrimSpace(data[:min(len(data), 512)])))
	return strings.Contains(prefix, "<html") || strings.Contains(prefix, "<!doctype html")
}

func extractHTMLText(data []byte) string {
	z := html.NewTokenizer(bytes.NewReader(data))
	var b strings.Builder
	if len(data) > 0 {
		b.Grow(min(len(data), 64*1024))
	}
	skipDepth := 0
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if errors.Is(z.Err(), io.EOF) {
				return b.String()
			}
			return b.String()
		case html.StartTagToken:
			name, _ := z.TagName()
			switch {
			case htmlSkipTag(name):
				skipDepth++
			case htmlBreakBeforeTag(name):
				b.WriteByte('\n')
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			switch {
			case htmlSkipTag(name):
				if skipDepth > 0 {
					skipDepth--
				}
			case htmlBreakAfterTag(name):
				b.WriteByte('\n')
			}
		case html.TextToken:
			if skipDepth == 0 {
				text := bytes.TrimSpace(z.Text())
				if len(text) != 0 {
					if b.Len() > 0 {
						b.WriteByte(' ')
					}
					b.Write(text)
				}
			}
		}
	}
}

func htmlSkipTag(name []byte) bool {
	switch len(name) {
	case 3:
		return htmlTagEqual(name, "svg")
	case 5:
		return htmlTagEqual(name, "style")
	case 6:
		return htmlTagEqual(name, "script")
	case 8:
		return htmlTagEqual(name, "noscript")
	default:
		return false
	}
}

func htmlBreakBeforeTag(name []byte) bool {
	switch len(name) {
	case 1:
		return htmlTagEqual(name, "p")
	case 2:
		return htmlTagEqual(name, "br") || htmlTagEqual(name, "li") || htmlTagEqual(name, "tr") || htmlTagEqual(name, "h1") || htmlTagEqual(name, "h2") || htmlTagEqual(name, "h3") || htmlTagEqual(name, "h4")
	case 3:
		return htmlTagEqual(name, "div")
	case 7:
		return htmlTagEqual(name, "section") || htmlTagEqual(name, "article")
	default:
		return false
	}
}

func htmlBreakAfterTag(name []byte) bool {
	switch len(name) {
	case 1:
		return htmlTagEqual(name, "p")
	case 2:
		return htmlTagEqual(name, "li") || htmlTagEqual(name, "tr")
	case 3:
		return htmlTagEqual(name, "div")
	case 7:
		return htmlTagEqual(name, "section") || htmlTagEqual(name, "article")
	default:
		return false
	}
}

func htmlTagEqual(name []byte, lower string) bool {
	if len(name) != len(lower) {
		return false
	}
	for i, c := range name {
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != lower[i] {
			return false
		}
	}
	return true
}

func extractPDFText(data []byte) string {
	var parts []string
	for i := 0; i < len(data); i++ {
		if data[i] != '(' {
			continue
		}
		i++
		var b strings.Builder
		escaped := false
		for ; i < len(data); i++ {
			c := data[i]
			if escaped {
				switch c {
				case 'n':
					b.WriteByte('\n')
				case 'r':
					b.WriteByte('\r')
				case 't':
					b.WriteByte('\t')
				default:
					b.WriteByte(c)
				}
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == ')' {
				break
			}
			if c >= 32 || c == '\n' || c == '\t' {
				b.WriteByte(c)
			}
		}
		text := strings.TrimSpace(b.String())
		if len(text) >= 3 && printableRatio(text) > 0.75 {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func printableRatio(text string) float64 {
	if text == "" {
		return 0
	}
	printable := 0
	total := 0
	for _, r := range text {
		total++
		if r == '\n' || r == '\t' || r >= 32 {
			printable++
		}
	}
	return float64(printable) / float64(total)
}

func compactWhitespace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func fallback(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

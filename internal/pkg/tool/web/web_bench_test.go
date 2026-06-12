package web

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkExtractHTMLTextLargeDocument(b *testing.B) {
	var html strings.Builder
	html.WriteString("<!doctype html><html><head><style>.x{}</style><script>ignored()</script></head><body>")
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&html, "<article><h2>Section %d</h2><p>%s</p><ul><li>item one</li><li>item two</li></ul></article>", i, strings.Repeat("content ", 20))
	}
	html.WriteString("</body></html>")
	data := []byte(html.String())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		text := extractHTMLText(data)
		if len(text) == 0 {
			b.Fatal("empty extracted text")
		}
	}
}

func BenchmarkFormatFetchResultLargeText(b *testing.B) {
	text := strings.Repeat("large extracted text with many words ", 5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out := formatFetchResult("https://example.test/final", "https://example.test/source", "text/html", "html", 200, len(text), false, text)
		if len(out) == 0 {
			b.Fatal("empty formatted result")
		}
	}
}

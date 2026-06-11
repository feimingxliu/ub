package search

import (
	"bufio"
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

const (
	// binarySniffBytes is how many bytes we inspect to decide whether a
	// file is binary. Matches ripgrep's default behavior.
	binarySniffBytes = 8 * 1024
	// maxLineLen caps each matched line in the output to avoid blowing
	// up the LLM context window.
	maxLineLen = 2048
)

type goBackend struct{}

func (g *goBackend) run(ctx context.Context, opts grepOpts) ([]grepHit, error) {
	var hits []grepHit
	err := filepath.WalkDir(opts.searchPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		rel, err := tool.RelToRoot(opts.root, path)
		if err != nil {
			return nil
		}

		if opts.include != "" {
			match, err := doublestar.PathMatch(opts.include, rel)
			if err != nil || !match {
				return nil
			}
		}

		fileHits, err := scanFile(path, rel, opts.pattern)
		if err != nil {
			return nil
		}
		hits = append(hits, fileHits...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}

// scanFile reads the file at abs and returns hits for every matching
// line. Binary files (detected via a NUL byte in the first 8KB) and
// files that fail to open are silently skipped.
func scanFile(abs, rel string, re *regexp.Regexp) ([]grepHit, error) {
	f, err := os.Open(abs)
	if err != nil {
		return nil, nil
	}
	defer f.Close()

	sniff := make([]byte, binarySniffBytes)
	n, _ := f.Read(sniff)
	if bytes.IndexByte(sniff[:n], 0) >= 0 {
		return nil, nil
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil, nil
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var hits []grepHit
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		text := scanner.Text()
		if !re.MatchString(text) {
			continue
		}
		hits = append(hits, grepHit{
			Path: rel,
			Line: lineNo,
			Text: truncateLine(text),
		})
	}
	return hits, nil
}

func truncateLine(s string) string {
	if len(s) <= maxLineLen {
		return s
	}
	return s[:maxLineLen] + " ...(truncated)"
}

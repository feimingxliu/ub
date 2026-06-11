package search

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

// commandRunner abstracts exec.Cmd so tests can stub out actual rg
// invocations and assert the constructed argv.
type commandRunner interface {
	output(ctx context.Context, root, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) output(ctx context.Context, root, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = root
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	// stderr is swallowed: rg uses it for non-fatal messages we already
	// suppress with --no-messages.
	cmd.Stderr = &bytes.Buffer{}
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			// rg exits 1 when there are no matches; not an error.
			return stdout.Bytes(), nil
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

type rgBackend struct {
	runner commandRunner
}

func newRgBackend() *rgBackend { return &rgBackend{runner: execRunner{}} }

func (r *rgBackend) run(ctx context.Context, opts grepOpts) ([]grepHit, error) {
	args := []string{
		"--line-number",
		"--no-heading",
		"--color=never",
		"--no-messages",
	}
	if opts.include != "" {
		args = append(args, "-g", opts.include)
	}
	args = append(args, opts.rawPattern, opts.searchPath)

	out, err := r.runner.output(ctx, opts.root, "rg", args...)
	if err != nil {
		return nil, fmt.Errorf("rg: %w", err)
	}
	return parseRipgrepOutput(out, opts.root)
}

// parseRipgrepOutput turns rg's `path:line:match` lines into grepHits.
// The path emitted by rg is relative to cmd.Dir (the workspace root),
// so we keep it after normalizing separators to '/'.
func parseRipgrepOutput(out []byte, root string) ([]grepHit, error) {
	var hits []grepHit
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		// rg output format: path:line:match
		first := strings.IndexByte(line, ':')
		if first < 0 {
			return nil, fmt.Errorf("rg: unexpected output %q", line)
		}
		second := strings.IndexByte(line[first+1:], ':')
		if second < 0 {
			return nil, fmt.Errorf("rg: unexpected output %q", line)
		}
		second += first + 1
		path := line[:first]
		lineNo, err := strconv.Atoi(line[first+1 : second])
		if err != nil {
			return nil, fmt.Errorf("rg: bad line number in %q: %w", line, err)
		}
		text := line[second+1:]
		// Normalize path: rg already emits forward slashes on POSIX,
		// but be defensive on Windows.
		rel, err := tool.RelToRoot(root, root+"/"+path)
		if err != nil {
			rel = path
		}
		hits = append(hits, grepHit{
			Path: rel,
			Line: lineNo,
			Text: truncateLine(text),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("rg: read output: %w", err)
	}
	return hits, nil
}

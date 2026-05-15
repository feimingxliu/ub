// Package search implements the grep code-search tool.
//
// The tool has two interchangeable backends:
//
//   - goBackend (default): a pure-Go implementation built on
//     filepath.WalkDir, regexp.Regexp and bufio.Scanner. Output is
//     deterministic and the binary has no runtime dependency on rg.
//   - rgBackend: shells out to ripgrep. Implemented for parity but not
//     enabled by default in V1 - the goBackend keeps CI reproducible
//     across machines that may or may not have rg installed. A future
//     iteration will expose a config switch.
//
// Both backends emit the same `path:line:match` text format with paths
// relative to the workspace root, so the LLM cannot tell which one ran.
package search

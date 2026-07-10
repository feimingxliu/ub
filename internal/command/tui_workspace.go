package command

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/feimingxliu/ub/internal/store"
)

type fileCandidate struct {
	path  string
	score int
}

func listWorkspaceFiles(ctx context.Context, root, query string, limit int) ([]string, error) {
	root = filepath.Clean(root)
	if limit <= 0 {
		limit = 50
	}
	query = strings.ToLower(strings.TrimSpace(query))
	var candidates []fileCandidate
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if path == root {
			return nil
		}
		if entry.IsDir() {
			if excludedFileMentionDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		score, ok := fileMentionMatchScore(rel, query)
		if !ok {
			return nil
		}
		candidates = append(candidates, fileCandidate{path: rel, score: score})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score < candidates[j].score
		}
		if len(candidates[i].path) != len(candidates[j].path) {
			return len(candidates[i].path) < len(candidates[j].path)
		}
		return candidates[i].path < candidates[j].path
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	out := make([]string, len(candidates))
	for i, candidate := range candidates {
		out[i] = candidate.path
	}
	return out, nil
}

func excludedFileMentionDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "vendor", "dist", "build":
		return true
	default:
		return false
	}
}

func fileMentionMatchScore(path, query string) (int, bool) {
	if query == "" {
		return 0, true
	}
	path = strings.ToLower(path)
	base := strings.ToLower(filepath.Base(path))
	switch {
	case strings.HasPrefix(path, query):
		return 0, true
	case strings.HasPrefix(base, query):
		return 1, true
	case strings.Contains(path, query):
		return 2, true
	default:
		return 0, false
	}
}

func listCurrentWorkspaceSessions(ctx context.Context, limit int) ([]store.Session, error) {
	path, err := store.DefaultPath()
	if err != nil {
		return nil, fmt.Errorf("locate session store: %w", err)
	}
	st, err := store.Open(path)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	cwd, err := currentWorkspace()
	if err != nil {
		return nil, err
	}
	return st.ListSessions(ctx, cwd, limit)
}

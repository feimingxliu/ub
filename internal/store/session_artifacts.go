package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/feimingxliu/ub/internal/paths"
	"github.com/feimingxliu/ub/internal/tooloutput"
)

const rolloutToolResultType = "tool_result"

var unsafeTodoPathPart = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

type deletionArtifacts struct {
	SessionIDs      []string
	TodoFiles       []string
	FileHistoryDirs []string
	ToolOutputDirs  []string
	ToolOutputFiles []string
	PlanFiles       []string
	PlanParentDirs  []string
}

type deletionToolResultPayload struct {
	Files          []deletionFileChange `json:"files,omitempty"`
	FullOutputPath string               `json:"full_output_path,omitempty"`
}

type deletionFileChange struct {
	Path string `json:"path"`
}

func (s *Store) sessionIDsForWorkspace(ctx context.Context, workspace string) ([]string, error) {
	return s.querySessionIDs(ctx, `SELECT id FROM sessions WHERE workspace = ?`, workspace)
}

func (s *Store) allSessionIDs(ctx context.Context) ([]string, error) {
	return s.querySessionIDs(ctx, `SELECT id FROM sessions`, nil)
}

func (s *Store) prunableSessionIDs(ctx context.Context, cutoff int64, minRecentPerWorkspace int) ([]string, error) {
	return s.querySessionIDs(ctx, `SELECT id FROM sessions
		WHERE updated_at < ?
		  AND (
		    SELECT COUNT(*)
		      FROM sessions AS newer
		     WHERE newer.workspace = sessions.workspace
		       AND (
		         newer.updated_at > sessions.updated_at
		         OR (newer.updated_at = sessions.updated_at AND newer.id > sessions.id)
		       )
		  ) >= ?`, cutoff, minRecentPerWorkspace)
}

func (s *Store) querySessionIDs(ctx context.Context, query string, args ...any) ([]string, error) {
	if len(args) == 1 && args[0] == nil {
		args = nil
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query session ids: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan session id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session ids: %w", err)
	}
	return ids, nil
}

func (s *Store) deletionArtifactsForSessionIDs(ctx context.Context, ids []string) (deletionArtifacts, error) {
	ids = uniqueNonEmpty(ids)
	if len(ids) == 0 {
		return deletionArtifacts{}, nil
	}
	stateRoot, err := paths.StateRoot()
	if err != nil {
		return deletionArtifacts{}, err
	}
	artifacts := deletionArtifacts{SessionIDs: ids}
	planRoot := filepath.Join(stateRoot, "plans")
	defaultToolOutputRoot := filepath.Join(stateRoot, "tool_outputs")
	planCandidates := map[string]struct{}{}
	toolOutputFiles := map[string]struct{}{}

	for _, id := range ids {
		artifacts.TodoFiles = append(artifacts.TodoFiles, filepath.Join(stateRoot, "todos", todoPathPart(id)+".json"))
		artifacts.FileHistoryDirs = append(artifacts.FileHistoryDirs, filepath.Join(stateRoot, "file-history", id))
		artifacts.ToolOutputDirs = append(artifacts.ToolOutputDirs, filepath.Join(defaultToolOutputRoot, tooloutput.SafePathPart(id)))

		payloads, err := s.toolResultPayloadsForSession(ctx, id)
		if err != nil {
			return deletionArtifacts{}, err
		}
		for _, payload := range payloads {
			if outputPath, ok := cleanPathUnderRoot(payload.FullOutputPath, defaultToolOutputRoot); ok {
				toolOutputFiles[outputPath] = struct{}{}
			}
			for _, file := range payload.Files {
				if planPath, ok := cleanPathUnderRoot(file.Path, planRoot); ok && filepath.Ext(planPath) == ".md" {
					planCandidates[planPath] = struct{}{}
				}
			}
		}
	}

	for path := range toolOutputFiles {
		artifacts.ToolOutputFiles = append(artifacts.ToolOutputFiles, path)
	}
	if len(planCandidates) > 0 {
		referenced, err := s.planFilesReferencedOutsideSessions(ctx, idSet(ids), planCandidates, planRoot)
		if err != nil {
			return deletionArtifacts{}, err
		}
		parentDirs := map[string]struct{}{}
		for path := range planCandidates {
			if referenced[path] {
				continue
			}
			artifacts.PlanFiles = append(artifacts.PlanFiles, path)
			parentDirs[filepath.Dir(path)] = struct{}{}
		}
		for dir := range parentDirs {
			artifacts.PlanParentDirs = append(artifacts.PlanParentDirs, dir)
		}
	}
	return artifacts, nil
}

func (s *Store) toolResultPayloadsForSession(ctx context.Context, sessionID string) ([]deletionToolResultPayload, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM events WHERE session_id = ? AND type = ?`, sessionID, rolloutToolResultType)
	if err != nil {
		return nil, fmt.Errorf("query tool result artifacts for session %s: %w", sessionID, err)
	}
	defer rows.Close()

	var payloads []deletionToolResultPayload
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan tool result artifacts for session %s: %w", sessionID, err)
		}
		var payload deletionToolResultPayload
		if err := json.Unmarshal(raw, &payload); err == nil {
			payloads = append(payloads, payload)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tool result artifacts for session %s: %w", sessionID, err)
	}
	return payloads, nil
}

func (s *Store) planFilesReferencedOutsideSessions(ctx context.Context, selected map[string]struct{}, candidates map[string]struct{}, planRoot string) (map[string]bool, error) {
	referenced := map[string]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT session_id, payload FROM events WHERE type = ?`, rolloutToolResultType)
	if err != nil {
		return nil, fmt.Errorf("query plan references: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var sessionID string
		var raw []byte
		if err := rows.Scan(&sessionID, &raw); err != nil {
			return nil, fmt.Errorf("scan plan reference: %w", err)
		}
		if _, deleting := selected[sessionID]; deleting {
			continue
		}
		var payload deletionToolResultPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			continue
		}
		for _, file := range payload.Files {
			path, ok := cleanPathUnderRoot(file.Path, planRoot)
			if !ok {
				continue
			}
			if _, candidate := candidates[path]; candidate {
				referenced[path] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plan references: %w", err)
	}
	return referenced, nil
}

func cleanupDeletionArtifacts(artifacts deletionArtifacts) error {
	for _, path := range artifacts.ToolOutputFiles {
		if err := removeFileIfExists(path); err != nil {
			return err
		}
	}
	for _, dir := range artifacts.ToolOutputDirs {
		if err := removeDirIfExists(dir); err != nil {
			return err
		}
	}
	for _, path := range artifacts.TodoFiles {
		if err := removeFileIfExists(path); err != nil {
			return err
		}
	}
	for _, dir := range artifacts.FileHistoryDirs {
		if err := removeDirIfExists(dir); err != nil {
			return err
		}
	}
	for _, path := range artifacts.PlanFiles {
		if err := removeFileIfExists(path); err != nil {
			return err
		}
	}
	for _, dir := range artifacts.PlanParentDirs {
		if err := removeEmptyDirIfExists(dir); err != nil {
			return err
		}
	}
	return nil
}

func removeFileIfExists(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

func removeDirIfExists(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.RemoveAll(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

func removeEmptyDirIfExists(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read dir %s: %w", path, err)
	}
	if len(entries) > 0 {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) && !isDirectoryNotEmpty(err) {
		return fmt.Errorf("remove empty dir %s: %w", path, err)
	}
	return nil
}

func isDirectoryNotEmpty(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "directory not empty") || strings.Contains(msg, "directory is not empty")
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func idSet(ids []string) map[string]struct{} {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set
}

func cleanPathUnderRoot(path, root string) (string, bool) {
	path = strings.TrimSpace(path)
	root = strings.TrimSpace(root)
	if path == "" || root == "" || !filepath.IsAbs(path) {
		return "", false
	}
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	return path, true
}

func todoPathPart(value string) string {
	value = strings.TrimSpace(value)
	value = unsafeTodoPathPart.ReplaceAllString(value, "_")
	value = strings.Trim(value, "._-")
	if value == "" {
		return "session"
	}
	if len(value) > 80 {
		value = value[:80]
		value = strings.Trim(value, "._-")
	}
	if value == "" {
		return "session"
	}
	return value
}

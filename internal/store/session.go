package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// CreateSession inserts a new session.
func (s *Store) CreateSession(ctx context.Context, sess Session) error {
	if sess.ID == "" {
		return errors.New("session id is empty")
	}
	if sess.Workspace == "" {
		return errors.New("session workspace is empty")
	}
	now := time.Now().UTC()
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = now
	}
	if sess.UpdatedAt.IsZero() {
		sess.UpdatedAt = sess.CreatedAt
	}
	_, err := s.db.ExecContext(
		ctx, `INSERT INTO sessions
		(id, workspace, title, created_at, updated_at, summary, model)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sess.ID,
		sess.Workspace,
		sess.Title,
		timeToMillis(sess.CreatedAt),
		timeToMillis(sess.UpdatedAt),
		sess.Summary,
		sess.Model,
	)
	if err != nil {
		return fmt.Errorf("create session %s: %w", sess.ID, err)
	}
	return nil
}

// GetSession returns one session by id.
func (s *Store) GetSession(ctx context.Context, id string) (*Session, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, workspace, title, created_at, updated_at, summary, model
		FROM sessions WHERE id = ?`, id)
	sess, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get session %s: %w", id, err)
	}
	return sess, nil
}

// ListSessions lists recent sessions for a workspace.
func (s *Store) ListSessions(ctx context.Context, workspace string, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, workspace, title, created_at, updated_at, summary, model
		FROM sessions
		WHERE workspace = ?
		ORDER BY updated_at DESC
		LIMIT ?`, workspace, limit)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		out = append(out, *sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}
	return out, nil
}

// UpdateSession updates a session by id.
func (s *Store) UpdateSession(ctx context.Context, sess Session) error {
	if sess.ID == "" {
		return errors.New("session id is empty")
	}
	if sess.UpdatedAt.IsZero() {
		sess.UpdatedAt = time.Now().UTC()
	}
	res, err := s.db.ExecContext(
		ctx, `UPDATE sessions
		SET workspace = ?, title = ?, created_at = ?, updated_at = ?, summary = ?, model = ?
		WHERE id = ?`,
		sess.Workspace,
		sess.Title,
		timeToMillis(sess.CreatedAt),
		timeToMillis(sess.UpdatedAt),
		sess.Summary,
		sess.Model,
		sess.ID,
	)
	if err != nil {
		return fmt.Errorf("update session %s: %w", sess.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update session %s rows affected: %w", sess.ID, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSession removes a session by id.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete session %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete session %s rows affected: %w", id, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteWorkspaceSessions removes all sessions for a workspace.
func (s *Store) DeleteWorkspaceSessions(ctx context.Context, workspace string) (int64, error) {
	if workspace == "" {
		return 0, errors.New("session workspace is empty")
	}
	res, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE workspace = ?", workspace)
	if err != nil {
		return 0, fmt.Errorf("delete workspace sessions %s: %w", workspace, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete workspace sessions %s rows affected: %w", workspace, err)
	}
	return n, nil
}

// PruneOptions controls age-based session pruning.
type PruneOptions struct {
	MaxAge                time.Duration
	MinRecentPerWorkspace int
	Now                   time.Time
}

// PruneResult reports what PruneSessions removed.
type PruneResult struct {
	Deleted int64
}

// PruneSessions deletes sessions older than MaxAge unless they are still among
// the newest MinRecentPerWorkspace sessions for their workspace. Events are
// removed by the schema's ON DELETE CASCADE relationship.
func (s *Store) PruneSessions(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	if opts.MaxAge <= 0 {
		return PruneResult{}, nil
	}
	if opts.MinRecentPerWorkspace < 0 {
		opts.MinRecentPerWorkspace = 0
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cutoff := timeToMillis(now.Add(-opts.MaxAge))
	res, err := s.db.ExecContext(
		ctx, `DELETE FROM sessions
		WHERE updated_at < ?
		  AND (
		    SELECT COUNT(*)
		      FROM sessions AS newer
		     WHERE newer.workspace = sessions.workspace
		       AND (
		         newer.updated_at > sessions.updated_at
		         OR (newer.updated_at = sessions.updated_at AND newer.id > sessions.id)
		       )
		  ) >= ?`,
		cutoff,
		opts.MinRecentPerWorkspace,
	)
	if err != nil {
		return PruneResult{}, fmt.Errorf("prune sessions: %w", err)
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return PruneResult{}, fmt.Errorf("prune sessions rows affected: %w", err)
	}
	return PruneResult{Deleted: deleted}, nil
}

type sessionScanner interface {
	Scan(dest ...any) error
}

func scanSession(row sessionScanner) (*Session, error) {
	var sess Session
	var createdAt, updatedAt int64
	if err := row.Scan(
		&sess.ID,
		&sess.Workspace,
		&sess.Title,
		&createdAt,
		&updatedAt,
		&sess.Summary,
		&sess.Model,
	); err != nil {
		return nil, err
	}
	sess.CreatedAt = millisToTime(createdAt)
	sess.UpdatedAt = millisToTime(updatedAt)
	return &sess, nil
}

func timeToMillis(t time.Time) int64 {
	return t.UTC().UnixMilli()
}

func millisToTime(ms int64) time.Time {
	return time.UnixMilli(ms).UTC()
}

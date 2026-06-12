package rollout

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/feimingxliu/ub/internal/pkg/workspace/store"
)

// Writer appends rollout events.
type Writer interface {
	Append(ctx context.Context, event Event) error
	Close() error
}

// Reader reads rollout events.
type Reader interface {
	ForEach(ctx context.Context, sessionID string, fn func(Event) error) error
}

// SQLite provides rollout read/write operations over the shared store DB.
type SQLite struct {
	db     *sql.DB
	insert *sql.Stmt
	mu     sync.RWMutex
}

// New creates a SQLite rollout reader/writer bound to an opened store.
func New(st *store.Store) (*SQLite, error) {
	if st == nil || st.DB() == nil {
		return nil, errors.New("rollout store is nil")
	}
	insert, err := st.DB().PrepareContext(context.Background(), appendEventSQL)
	if err != nil {
		return nil, fmt.Errorf("prepare rollout append: %w", err)
	}
	return &SQLite{db: st.DB(), insert: insert}, nil
}

const appendEventSQL = `INSERT INTO events
	(id, session_id, turn, time, type, payload)
	VALUES (?, ?, ?, ?, ?, ?)`

// Append inserts one rollout event.
func (s *SQLite) Append(ctx context.Context, event Event) error {
	if err := validateEvent(event); err != nil {
		return err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.insert == nil {
		return errors.New("rollout writer is closed")
	}
	if _, err := s.insert.ExecContext(
		ctx,
		event.ID,
		event.SessionID,
		event.Turn,
		event.Time.UTC().UnixMilli(),
		string(event.Type),
		[]byte(event.Payload),
	); err != nil {
		return fmt.Errorf("append rollout event %s: %w", event.ID, err)
	}
	return nil
}

// ForEach reads events for one session in stable order.
func (s *SQLite) ForEach(ctx context.Context, sessionID string, fn func(Event) error) error {
	if sessionID == "" {
		return errors.New("rollout session id is empty")
	}
	if fn == nil {
		return errors.New("rollout reader callback is nil")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, turn, time, type, payload
		FROM events
		WHERE session_id = ?
		ORDER BY turn ASC, time ASC, rowid ASC`, sessionID)
	if err != nil {
		return fmt.Errorf("read rollout events: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return err
		}
		if err := fn(event); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rollout events: %w", err)
	}
	return nil
}

// ForEachFromTurn reads events for one session at or after startTurn in stable
// order.
func (s *SQLite) ForEachFromTurn(ctx context.Context, sessionID string, startTurn int, fn func(Event) error) error {
	if sessionID == "" {
		return errors.New("rollout session id is empty")
	}
	if startTurn <= 0 {
		return errors.New("rollout start turn must be positive")
	}
	if fn == nil {
		return errors.New("rollout reader callback is nil")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, turn, time, type, payload
		FROM events
		WHERE session_id = ? AND turn >= ?
		ORDER BY turn ASC, time ASC, rowid ASC`, sessionID, startTurn)
	if err != nil {
		return fmt.Errorf("read rollout events: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return err
		}
		if err := fn(event); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rollout events: %w", err)
	}
	return nil
}

// DeleteFromTurn deletes events for one session at or after startTurn and
// returns the number of deleted rows.
func (s *SQLite) DeleteFromTurn(ctx context.Context, sessionID string, startTurn int) (int, error) {
	if sessionID == "" {
		return 0, errors.New("rollout session id is empty")
	}
	if startTurn <= 0 {
		return 0, errors.New("rollout start turn must be positive")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE session_id = ? AND turn >= ?`, sessionID, startTurn)
	if err != nil {
		return 0, fmt.Errorf("delete rollout events from turn %d: %w", startTurn, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count deleted rollout events: %w", err)
	}
	return int(count), nil
}

// Close is present to satisfy Writer. The underlying store owns the DB handle.
func (s *SQLite) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.insert == nil {
		return nil
	}
	err := s.insert.Close()
	s.insert = nil
	return err
}

func validateEvent(event Event) error {
	if event.ID == "" {
		return errors.New("rollout event id is empty")
	}
	if event.SessionID == "" {
		return errors.New("rollout event session id is empty")
	}
	if event.Turn <= 0 {
		return errors.New("rollout event turn must be positive")
	}
	if event.Type == "" {
		return errors.New("rollout event type is empty")
	}
	if event.Time.IsZero() {
		return errors.New("rollout event time is empty")
	}
	if len(event.Payload) == 0 {
		return errors.New("rollout event payload is empty")
	}
	return nil
}

type eventScanner interface {
	Scan(dest ...any) error
}

func scanEvent(row eventScanner) (Event, error) {
	var event Event
	var ms int64
	var typ string
	var payload []byte
	if err := row.Scan(&event.ID, &event.SessionID, &event.Turn, &ms, &typ, &payload); err != nil {
		return Event{}, fmt.Errorf("scan rollout event: %w", err)
	}
	event.Time = time.UnixMilli(ms).UTC()
	event.Type = Type(typ)
	event.Payload = append([]byte(nil), payload...)
	return event, nil
}

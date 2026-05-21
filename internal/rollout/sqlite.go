package rollout

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/feimingxliu/ub/internal/store"
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
	db *sql.DB
}

// New creates a SQLite rollout reader/writer bound to an opened store.
func New(st *store.Store) (*SQLite, error) {
	if st == nil || st.DB() == nil {
		return nil, errors.New("rollout store is nil")
	}
	return &SQLite{db: st.DB()}, nil
}

// Append inserts one rollout event.
func (s *SQLite) Append(ctx context.Context, event Event) error {
	if err := validateEvent(event); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(
		ctx, `INSERT INTO events
		(id, session_id, turn, time, type, payload)
		VALUES (?, ?, ?, ?, ?, ?)`,
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
		ORDER BY turn ASC, time ASC, id ASC`, sessionID)
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

// Close is present to satisfy Writer. The underlying store owns the DB handle.
func (s *SQLite) Close() error {
	return nil
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

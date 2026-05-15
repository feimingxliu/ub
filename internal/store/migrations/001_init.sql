CREATE TABLE IF NOT EXISTS schema_version (
    version    INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    applied_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT PRIMARY KEY,
    workspace  TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    summary    TEXT NOT NULL DEFAULT '',
    model      TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_sessions_ws_updated
    ON sessions(workspace, updated_at DESC);

CREATE TABLE IF NOT EXISTS events (
    id         TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    turn       INTEGER NOT NULL,
    time       INTEGER NOT NULL,
    type       TEXT NOT NULL,
    payload    BLOB NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_session
    ON events(session_id, turn, time);

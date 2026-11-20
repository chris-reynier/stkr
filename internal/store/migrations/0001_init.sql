-- 0001_init: every v1 table, indexes and the seed contexts.
-- Timestamps are TEXT, RFC3339 UTC with milliseconds.

CREATE TABLE IF NOT EXISTS contexts (
    id           TEXT PRIMARY KEY,
    label        TEXT NOT NULL,
    kind         TEXT NOT NULL CHECK (kind IN ('project','mode','personal','system')),
    tempo_ticket TEXT,
    active       INTEGER NOT NULL DEFAULT 1,
    notebook_url TEXT,
    workdir      TEXT,
    icon         TEXT,
    sort         INTEGER
);

CREATE TABLE IF NOT EXISTS events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    ts         TEXT NOT NULL,
    context_id TEXT NOT NULL REFERENCES contexts(id),
    source     TEXT NOT NULL CHECK (source IN ('deck','cli','api','hook','mcp','ui','system')),
    note       TEXT,
    confirmed  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_events_ts ON events(ts);

CREATE TABLE IF NOT EXISTS agent_instances (
    id           TEXT PRIMARY KEY,
    slot         INTEGER,
    context_id   TEXT REFERENCES contexts(id),
    workspace    TEXT,
    window_title TEXT,
    state        TEXT NOT NULL,
    summary      TEXT,
    updated      TEXT NOT NULL,
    queued       INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS ideas (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    ts                TEXT,
    context_id        TEXT,
    tag               TEXT NOT NULL DEFAULT 'pro',
    raw_text          TEXT,
    clean_text        TEXT,
    title             TEXT,
    suggested_project TEXT,
    status            TEXT NOT NULL DEFAULT 'new',
    audio_path        TEXT,
    source            TEXT,
    ai_state          TEXT NOT NULL DEFAULT 'pending'
);
CREATE INDEX IF NOT EXISTS idx_ideas_status ON ideas(status);
CREATE INDEX IF NOT EXISTS idx_ideas_ai_state ON ideas(ai_state);

CREATE TABLE IF NOT EXISTS bookmarks (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    title   TEXT,
    url     TEXT NOT NULL UNIQUE,
    tags    TEXT,
    pinned  INTEGER DEFAULT 0,
    created TEXT
);

CREATE TABLE IF NOT EXISTS items (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    date        TEXT,
    project     TEXT,
    type        TEXT CHECK (type IN ('action','decision','note','followup','risk')),
    summary     TEXT NOT NULL,
    source      TEXT,
    due         TEXT,
    origin      TEXT,
    fingerprint TEXT NOT NULL UNIQUE,
    status      TEXT DEFAULT 'open'
);
CREATE INDEX IF NOT EXISTS idx_items_date ON items(date);

CREATE TABLE IF NOT EXISTS days (
    date        TEXT PRIMARY KEY,
    top1        TEXT,
    top2        TEXT,
    top3        TEXT,
    brief_saved INTEGER DEFAULT 0,
    close_saved INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS kv (
    key   TEXT PRIMARY KEY,
    value TEXT
);

INSERT OR IGNORE INTO contexts (id, label, kind, active, sort) VALUES
    ('off',      'Off',      'system',   1, 0),
    ('break',    'Break',    'personal', 1, 10),
    ('planning', 'Planning', 'mode',     1, 20),
    ('meeting',  'Meeting',  'mode',     1, 30),
    ('other',    'Other',    'project',  1, 40);

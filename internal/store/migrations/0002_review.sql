-- 0002_review: review record types, local activity evidence and the time a
-- close was saved.

-- Items gain the review types achievement and gap. SQLite cannot change a
-- CHECK constraint, so the table is rebuilt with the same rows and ids.
CREATE TABLE items_new (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    date        TEXT,
    project     TEXT,
    type        TEXT CHECK (type IN ('action','decision','note','followup','risk','achievement','gap')),
    summary     TEXT NOT NULL,
    source      TEXT,
    due         TEXT,
    origin      TEXT,
    fingerprint TEXT NOT NULL UNIQUE,
    status      TEXT DEFAULT 'open'
);
INSERT INTO items_new (id, date, project, type, summary, source, due, origin, fingerprint, status)
    SELECT id, date, project, type, summary, source, due, origin, fingerprint, status FROM items;
DROP TABLE items;
ALTER TABLE items_new RENAME TO items;
CREATE INDEX IF NOT EXISTS idx_items_date ON items(date);

ALTER TABLE days ADD COLUMN close_saved_at TEXT;

-- Activity evidence sent by a local client (the Stream Deck plugin). id is
-- the event identity (hash of ts, app, category, workspace and lock state).
CREATE TABLE IF NOT EXISTS activity_events (
    id               TEXT PRIMARY KEY,
    ts               TEXT NOT NULL,
    app              TEXT NOT NULL,
    category         TEXT NOT NULL CHECK (category IN ('development','meeting','email','administration','browser','break','lock','other')),
    workspace        TEXT,
    context_id       TEXT,
    duration_seconds INTEGER NOT NULL CHECK (duration_seconds > 0),
    lock_state       TEXT NOT NULL CHECK (lock_state IN ('active','idle','locked','break')),
    received         TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_activity_ts ON activity_events(ts);

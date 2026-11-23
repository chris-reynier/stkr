package store

// SQL statements. Keep all SQL in this package.
const (
	sqlListTables = `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`

	sqlCreateMigrations = `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT)`
	sqlSchemaVersion    = `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`
	sqlInsertMigration  = `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`

	sqlGetKV = `SELECT value FROM kv WHERE key = ?`
	sqlSetKV = `INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`

	contextCols        = `id, label, kind, tempo_ticket, active, notebook_url, workdir, icon, sort`
	sqlGetContext      = `SELECT ` + contextCols + ` FROM contexts WHERE id = ?`
	sqlListContexts    = `SELECT ` + contextCols + ` FROM contexts ORDER BY COALESCE(sort, 1000000), id`
	sqlListActiveCtx   = `SELECT ` + contextCols + ` FROM contexts WHERE active = 1 ORDER BY COALESCE(sort, 1000000), id`
	sqlInsertContext   = `INSERT INTO contexts (` + contextCols + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	sqlUpdateContext   = `UPDATE contexts SET label = ?, kind = ?, tempo_ticket = ?, active = ?, notebook_url = ?, workdir = ?, icon = ?, sort = ? WHERE id = ?`
	sqlSetContextActiv = `UPDATE contexts SET active = ? WHERE id = ?`

	eventCols           = `id, ts, context_id, source, note, confirmed`
	sqlInsertEvent      = `INSERT INTO events (ts, context_id, source, note, confirmed) VALUES (?, ?, ?, ?, ?)`
	sqlGetEvent         = `SELECT ` + eventCols + ` FROM events WHERE id = ?`
	sqlLastEventAtOrBef = `SELECT ` + eventCols + ` FROM events WHERE ts <= ? ORDER BY ts DESC, id DESC LIMIT 1`
	sqlEventsBetween    = `SELECT ` + eventCols + ` FROM events WHERE ts >= ? AND ts < ? ORDER BY ts, id`
	sqlNextEvent        = `SELECT ` + eventCols + ` FROM events WHERE ts > ? OR (ts = ? AND id > ?) ORDER BY ts, id LIMIT 1`
	sqlPrevEvent        = `SELECT ` + eventCols + ` FROM events WHERE ts < ? OR (ts = ? AND id < ?) ORDER BY ts DESC, id DESC LIMIT 1`
	sqlUpdateEvent      = `UPDATE events SET ts = ?, context_id = ?, source = ?, note = ?, confirmed = ? WHERE id = ?`
	sqlDeleteEvent      = `DELETE FROM events WHERE id = ?`

	sqlGetDay    = `SELECT date, top1, top2, top3, brief_saved, close_saved, close_saved_at FROM days WHERE date = ?`
	sqlUpsertDay = `INSERT INTO days (date, top1, top2, top3, brief_saved, close_saved, close_saved_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(date) DO UPDATE SET top1 = excluded.top1, top2 = excluded.top2, top3 = excluded.top3,
		brief_saved = excluded.brief_saved, close_saved = excluded.close_saved, close_saved_at = excluded.close_saved_at`

	activityCols      = `id, ts, app, category, workspace, context_id, duration_seconds, lock_state, received`
	sqlUpsertActivity = `INSERT INTO activity_events (` + activityCols + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET duration_seconds = MAX(activity_events.duration_seconds, excluded.duration_seconds),
		context_id = COALESCE(excluded.context_id, activity_events.context_id)`
	sqlGetActivity       = `SELECT duration_seconds, context_id FROM activity_events WHERE id = ?`
	sqlActivityBetween   = `SELECT ` + activityCols + ` FROM activity_events WHERE ts >= ? AND ts < ? ORDER BY ts, id`
	sqlDeleteActivityBef = `DELETE FROM activity_events WHERE ts < ?`

	agentCols      = `id, slot, context_id, workspace, window_title, state, summary, updated, queued`
	sqlGetAgent    = `SELECT ` + agentCols + ` FROM agent_instances WHERE id = ?`
	sqlListAgents  = `SELECT ` + agentCols + ` FROM agent_instances ORDER BY COALESCE(slot, 1000000), id`
	sqlUpsertAgent = `INSERT INTO agent_instances (` + agentCols + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET slot = excluded.slot, context_id = excluded.context_id, workspace = excluded.workspace,
		window_title = excluded.window_title, state = excluded.state, summary = excluded.summary,
		updated = excluded.updated, queued = excluded.queued`
	sqlDeleteAgent = `DELETE FROM agent_instances WHERE id = ?`

	ideaCols          = `id, ts, context_id, tag, raw_text, clean_text, title, suggested_project, status, audio_path, source, ai_state`
	sqlInsertIdea     = `INSERT INTO ideas (ts, context_id, tag, raw_text, clean_text, title, suggested_project, status, audio_path, source, ai_state) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	sqlGetIdea        = `SELECT ` + ideaCols + ` FROM ideas WHERE id = ?`
	sqlListIdeas      = `SELECT ` + ideaCols + ` FROM ideas ORDER BY ts DESC, id DESC`
	sqlListIdeasByStA = `SELECT ` + ideaCols + ` FROM ideas WHERE status = ? ORDER BY ts DESC, id DESC`
	sqlListIdeasByAI  = `SELECT ` + ideaCols + ` FROM ideas WHERE ai_state = ? ORDER BY ts, id`
	sqlUpdateIdea     = `UPDATE ideas SET ts = ?, context_id = ?, tag = ?, raw_text = ?, clean_text = ?, title = ?, suggested_project = ?, status = ?, audio_path = ?, source = ?, ai_state = ? WHERE id = ?`

	bookmarkCols      = `id, title, url, tags, pinned, created`
	sqlInsertBookmark = `INSERT INTO bookmarks (title, url, tags, pinned, created) VALUES (?, ?, ?, ?, ?)`
	sqlGetBookmark    = `SELECT ` + bookmarkCols + ` FROM bookmarks WHERE id = ?`
	sqlGetBookmarkURL = `SELECT ` + bookmarkCols + ` FROM bookmarks WHERE url = ?`
	sqlListBookmarks  = `SELECT ` + bookmarkCols + ` FROM bookmarks ORDER BY pinned DESC, created DESC, id DESC`
	sqlUpdateBookmark = `UPDATE bookmarks SET title = ?, url = ?, tags = ?, pinned = ? WHERE id = ?`
	sqlDeleteBookmark = `DELETE FROM bookmarks WHERE id = ?`

	itemCols           = `id, date, project, type, summary, source, due, origin, fingerprint, status`
	sqlInsertItem      = `INSERT INTO items (date, project, type, summary, source, due, origin, fingerprint, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(fingerprint) DO NOTHING`
	sqlGetItem         = `SELECT ` + itemCols + ` FROM items WHERE id = ?`
	sqlGetItemByFP     = `SELECT ` + itemCols + ` FROM items WHERE fingerprint = ?`
	sqlListItems       = `SELECT ` + itemCols + ` FROM items WHERE (? = '' OR date = ?) AND (? = '' OR status = ?) ORDER BY date, id`
	sqlUpdateItemState = `UPDATE items SET status = ? WHERE id = ?`
)

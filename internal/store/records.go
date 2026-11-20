package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/chris-reynier/stkr/internal/domain"
)

// ---- days ----

// GetDay returns the day row for date (YYYY-MM-DD). A missing row yields an
// empty Day with that date and no error.
func (s *Store) GetDay(ctx context.Context, date string) (domain.Day, error) {
	d := domain.Day{Date: date}
	var t1, t2, t3 sql.NullString
	var brief, closed sql.NullInt64
	err := s.q.QueryRowContext(ctx, sqlGetDay, date).Scan(&d.Date, &t1, &t2, &t3, &brief, &closed)
	if errors.Is(err, sql.ErrNoRows) {
		return d, nil
	}
	if err != nil {
		return d, fmt.Errorf("get day %s: %w", date, err)
	}
	d.Top = [3]string{t1.String, t2.String, t3.String}
	d.BriefSaved, d.CloseSaved = brief.Int64 != 0, closed.Int64 != 0
	return d, nil
}

// UpsertDay writes d.
func (s *Store) UpsertDay(ctx context.Context, d domain.Day) error {
	_, err := s.q.ExecContext(ctx, sqlUpsertDay, d.Date, nullStr(d.Top[0]), nullStr(d.Top[1]), nullStr(d.Top[2]),
		b2i(d.BriefSaved), b2i(d.CloseSaved))
	if err != nil {
		return fmt.Errorf("upsert day %s: %w", d.Date, err)
	}
	return nil
}

// ---- agent instances ----

func scanAgent(r scanner) (domain.AgentInstance, error) {
	var (
		a                                  domain.AgentInstance
		slot                               sql.NullInt64
		ctxID, ws, title, summary, updated sql.NullString
		state                              string
		queued                             int
	)
	if err := r.Scan(&a.ID, &slot, &ctxID, &ws, &title, &state, &summary, &updated, &queued); err != nil {
		return a, err
	}
	t, err := parseOptTS(updated)
	if err != nil {
		return a, err
	}
	a.Slot, a.ContextID, a.Workspace, a.WindowTitle = int(slot.Int64), ctxID.String, ws.String, title.String
	a.State, a.Summary, a.Updated, a.Queued = domain.AgentState(state), summary.String, t, queued != 0
	return a, nil
}

// UpsertAgent inserts or replaces the agent instance a.ID.
func (s *Store) UpsertAgent(ctx context.Context, a domain.AgentInstance) error {
	_, err := s.q.ExecContext(ctx, sqlUpsertAgent, a.ID, a.Slot, nullStr(a.ContextID), nullStr(a.Workspace),
		nullStr(a.WindowTitle), string(a.State), nullStr(a.Summary), FormatTS(a.Updated), b2i(a.Queued))
	if err != nil {
		return fmt.Errorf("upsert agent %s: %w", a.ID, err)
	}
	return nil
}

// GetAgent returns the agent instance with id, or ErrNotFound.
func (s *Store) GetAgent(ctx context.Context, id string) (domain.AgentInstance, error) {
	a, err := scanAgent(s.q.QueryRowContext(ctx, sqlGetAgent, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, notFound("agent", id)
	}
	return a, err
}

// ListAgents returns all agent instances ordered by slot.
func (s *Store) ListAgents(ctx context.Context) ([]domain.AgentInstance, error) {
	rows, err := s.q.QueryContext(ctx, sqlListAgents)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()
	out := []domain.AgentInstance{}
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteAgent removes the agent instance with id.
func (s *Store) DeleteAgent(ctx context.Context, id string) error {
	res, err := s.q.ExecContext(ctx, sqlDeleteAgent, id)
	if err != nil {
		return fmt.Errorf("delete agent %s: %w", id, err)
	}
	return mustAffect(res, "agent", id)
}

// ---- ideas ----

func scanIdea(r scanner) (domain.Idea, error) {
	var (
		i                                              domain.Idea
		ts, ctxID, raw, clean, title, proj, audio, src sql.NullString
		tag, status, ai                                string
	)
	if err := r.Scan(&i.ID, &ts, &ctxID, &tag, &raw, &clean, &title, &proj, &status, &audio, &src, &ai); err != nil {
		return i, err
	}
	t, err := parseOptTS(ts)
	if err != nil {
		return i, err
	}
	i.TS, i.ContextID, i.Tag, i.RawText, i.CleanText = t, ctxID.String, tag, raw.String, clean.String
	i.Title, i.SuggestedProject, i.Status = title.String, proj.String, domain.IdeaStatus(status)
	i.AudioPath, i.Source, i.AIState = audio.String, src.String, domain.AIState(ai)
	return i, nil
}

func ideaDefaults(i *domain.Idea) {
	if i.Tag == "" {
		i.Tag = "pro"
	}
	if i.Status == "" {
		i.Status = domain.IdeaNew
	}
	if i.AIState == "" {
		i.AIState = domain.AIPending
	}
}

// InsertIdea stores i and returns it with its id. Empty tag, status and AI
// state get their defaults.
func (s *Store) InsertIdea(ctx context.Context, i domain.Idea) (domain.Idea, error) {
	ideaDefaults(&i)
	res, err := s.q.ExecContext(ctx, sqlInsertIdea, FormatTS(i.TS), nullStr(i.ContextID), i.Tag, nullStr(i.RawText),
		nullStr(i.CleanText), nullStr(i.Title), nullStr(i.SuggestedProject), string(i.Status), nullStr(i.AudioPath),
		nullStr(i.Source), string(i.AIState))
	if err != nil {
		return i, fmt.Errorf("insert idea: %w", err)
	}
	i.ID, err = res.LastInsertId()
	return i, err
}

// GetIdea returns the idea with id, or ErrNotFound.
func (s *Store) GetIdea(ctx context.Context, id int64) (domain.Idea, error) {
	i, err := scanIdea(s.q.QueryRowContext(ctx, sqlGetIdea, id))
	if errors.Is(err, sql.ErrNoRows) {
		return i, notFound("idea", id)
	}
	return i, err
}

// ListIdeas returns ideas, newest first, optionally filtered by status.
func (s *Store) ListIdeas(ctx context.Context, status domain.IdeaStatus) ([]domain.Idea, error) {
	if status == "" {
		return s.listIdeas(ctx, sqlListIdeas)
	}
	return s.listIdeas(ctx, sqlListIdeasByStA, string(status))
}

// ListIdeasByAIState returns ideas with the given AI state, oldest first.
func (s *Store) ListIdeasByAIState(ctx context.Context, st domain.AIState) ([]domain.Idea, error) {
	return s.listIdeas(ctx, sqlListIdeasByAI, string(st))
}

func (s *Store) listIdeas(ctx context.Context, q string, args ...any) ([]domain.Idea, error) {
	rows, err := s.q.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list ideas: %w", err)
	}
	defer rows.Close()
	out := []domain.Idea{}
	for rows.Next() {
		i, err := scanIdea(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// UpdateIdea overwrites every column of the idea i.ID.
func (s *Store) UpdateIdea(ctx context.Context, i domain.Idea) error {
	ideaDefaults(&i)
	res, err := s.q.ExecContext(ctx, sqlUpdateIdea, FormatTS(i.TS), nullStr(i.ContextID), i.Tag, nullStr(i.RawText),
		nullStr(i.CleanText), nullStr(i.Title), nullStr(i.SuggestedProject), string(i.Status), nullStr(i.AudioPath),
		nullStr(i.Source), string(i.AIState), i.ID)
	if err != nil {
		return fmt.Errorf("update idea %d: %w", i.ID, err)
	}
	return mustAffect(res, "idea", i.ID)
}

// ---- bookmarks ----

func scanBookmark(r scanner) (domain.Bookmark, error) {
	var (
		b                    domain.Bookmark
		title, tags, created sql.NullString
		pinned               sql.NullInt64
	)
	if err := r.Scan(&b.ID, &title, &b.URL, &tags, &pinned, &created); err != nil {
		return b, err
	}
	t, err := parseOptTS(created)
	if err != nil {
		return b, err
	}
	b.Title, b.Pinned, b.Created, b.Tags = title.String, pinned.Int64 != 0, t, splitTags(tags.String)
	return b, nil
}

func splitTags(s string) []string {
	out := []string{}
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// InsertBookmark stores b. A duplicate URL returns ErrConflict.
func (s *Store) InsertBookmark(ctx context.Context, b domain.Bookmark) (domain.Bookmark, error) {
	res, err := s.q.ExecContext(ctx, sqlInsertBookmark, nullStr(b.Title), b.URL, nullStr(strings.Join(b.Tags, ",")),
		b2i(b.Pinned), FormatTS(b.Created))
	if isUnique(err) {
		return b, domain.New(domain.ErrConflict, "bookmark already exists: "+b.URL)
	}
	if err != nil {
		return b, fmt.Errorf("insert bookmark: %w", err)
	}
	b.ID, err = res.LastInsertId()
	return b, err
}

// GetBookmark returns the bookmark with id, or ErrNotFound.
func (s *Store) GetBookmark(ctx context.Context, id int64) (domain.Bookmark, error) {
	b, err := scanBookmark(s.q.QueryRowContext(ctx, sqlGetBookmark, id))
	if errors.Is(err, sql.ErrNoRows) {
		return b, notFound("bookmark", id)
	}
	return b, err
}

// GetBookmarkByURL returns the bookmark for url, or ErrNotFound.
func (s *Store) GetBookmarkByURL(ctx context.Context, url string) (domain.Bookmark, error) {
	b, err := scanBookmark(s.q.QueryRowContext(ctx, sqlGetBookmarkURL, url))
	if errors.Is(err, sql.ErrNoRows) {
		return b, notFound("bookmark", url)
	}
	return b, err
}

// ListBookmarks returns bookmarks, pinned first, then newest.
func (s *Store) ListBookmarks(ctx context.Context) ([]domain.Bookmark, error) {
	rows, err := s.q.QueryContext(ctx, sqlListBookmarks)
	if err != nil {
		return nil, fmt.Errorf("list bookmarks: %w", err)
	}
	defer rows.Close()
	out := []domain.Bookmark{}
	for rows.Next() {
		b, err := scanBookmark(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// UpdateBookmark overwrites title, url, tags and pinned of b.ID.
func (s *Store) UpdateBookmark(ctx context.Context, b domain.Bookmark) error {
	res, err := s.q.ExecContext(ctx, sqlUpdateBookmark, nullStr(b.Title), b.URL, nullStr(strings.Join(b.Tags, ",")), b2i(b.Pinned), b.ID)
	if isUnique(err) {
		return domain.New(domain.ErrConflict, "bookmark already exists: "+b.URL)
	}
	if err != nil {
		return fmt.Errorf("update bookmark %d: %w", b.ID, err)
	}
	return mustAffect(res, "bookmark", b.ID)
}

// DeleteBookmark removes the bookmark with id.
func (s *Store) DeleteBookmark(ctx context.Context, id int64) error {
	res, err := s.q.ExecContext(ctx, sqlDeleteBookmark, id)
	if err != nil {
		return fmt.Errorf("delete bookmark %d: %w", id, err)
	}
	return mustAffect(res, "bookmark", id)
}

// ---- items ----

func scanItem(r scanner) (domain.Item, error) {
	var (
		it                                        domain.Item
		date, proj, typ, src, due, origin, status sql.NullString
	)
	if err := r.Scan(&it.ID, &date, &proj, &typ, &it.Summary, &src, &due, &origin, &it.Fingerprint, &status); err != nil {
		return it, err
	}
	it.Date, it.Project, it.Type, it.Source = date.String, proj.String, domain.ItemType(typ.String), src.String
	it.Due, it.Origin, it.Status = due.String, origin.String, status.String
	return it, nil
}

// InsertItem stores it unless an item with the same fingerprint exists. It
// returns the stored item (new or existing) and whether it was created.
func (s *Store) InsertItem(ctx context.Context, it domain.Item) (domain.Item, bool, error) {
	if it.Status == "" {
		it.Status = "open"
	}
	res, err := s.q.ExecContext(ctx, sqlInsertItem, nullStr(it.Date), nullStr(it.Project), string(it.Type), it.Summary,
		nullStr(it.Source), nullStr(it.Due), nullStr(it.Origin), it.Fingerprint, it.Status)
	if err != nil {
		return it, false, fmt.Errorf("insert item: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		existing, err := scanItem(s.q.QueryRowContext(ctx, sqlGetItemByFP, it.Fingerprint))
		return existing, false, err
	}
	it.ID, err = res.LastInsertId()
	return it, true, err
}

// GetItem returns the item with id, or ErrNotFound.
func (s *Store) GetItem(ctx context.Context, id int64) (domain.Item, error) {
	it, err := scanItem(s.q.QueryRowContext(ctx, sqlGetItem, id))
	if errors.Is(err, sql.ErrNoRows) {
		return it, notFound("item", id)
	}
	return it, err
}

// ListItems returns items filtered by date and status ("" matches any).
func (s *Store) ListItems(ctx context.Context, date, status string) ([]domain.Item, error) {
	rows, err := s.q.QueryContext(ctx, sqlListItems, date, date, status, status)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	defer rows.Close()
	out := []domain.Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// SetItemStatus changes the status of item id.
func (s *Store) SetItemStatus(ctx context.Context, id int64, status string) error {
	res, err := s.q.ExecContext(ctx, sqlUpdateItemState, status, id)
	if err != nil {
		return fmt.Errorf("set item %d status: %w", id, err)
	}
	return mustAffect(res, "item", id)
}

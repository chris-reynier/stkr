package service

import (
	"context"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/chris-reynier/stkr/internal/capture"
	"github.com/chris-reynier/stkr/internal/domain"
)

// MaxURLLen is the longest accepted bookmark URL.
const MaxURLLen = 2048

// DuplicateBookmarkError is returned (as ErrConflict) when the URL is saved
// already; Existing is the stored bookmark.
type DuplicateBookmarkError struct{ Existing domain.Bookmark }

func (e *DuplicateBookmarkError) Error() string {
	return "bookmark already exists: " + e.Existing.URL
}

// Is makes errors.Is(err, domain.ErrConflict) true.
func (e *DuplicateBookmarkError) Is(target error) bool { return target == domain.ErrConflict }

// ParseBookmarkURL checks that raw is an absolute http(s) URL without
// embedded credentials and returns it trimmed.
func ParseBookmarkURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", invalid("URL is empty")
	}
	if len(raw) > MaxURLLen {
		return "", invalid("URL is longer than %d characters", MaxURLLen)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", invalid("not an absolute http or https URL")
	}
	if u.User != nil {
		return "", invalid("URLs with a user name or password are not accepted")
	}
	return raw, nil
}

// cleanTags lowercases, trims, de-duplicates and splits comma lists.
func cleanTags(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, group := range in {
		for _, t := range strings.Split(group, ",") {
			t = strings.ToLower(strings.TrimSpace(t))
			if t == "" || seen[t] {
				continue
			}
			if utf8.RuneCountInString(t) > 40 {
				t = string([]rune(t)[:40])
			}
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// AddBookmark saves rawURL. The title is read from the page within 2 seconds
// when bookmarks.fetch_title is on; on any failure the URL is the title. A
// saved URL returns *DuplicateBookmarkError.
func (s *Service) AddBookmark(ctx context.Context, rawURL string, tags []string) (domain.Bookmark, error) {
	u, err := ParseBookmarkURL(rawURL)
	if err != nil {
		return domain.Bookmark{}, err
	}
	if ex, err := s.st.GetBookmarkByURL(ctx, u); err == nil {
		return domain.Bookmark{}, &DuplicateBookmarkError{Existing: ex}
	}
	title := u
	if s.cfg.Bookmarks.FetchTitle {
		t, err := capture.FetchTitle(ctx, u, s.capt.titleTimeout)
		if err != nil {
			s.log.Info("bookmark title not fetched", "host", hostOf(u))
		} else if t != "" {
			title = t
		}
	}
	b, err := s.st.InsertBookmark(ctx, domain.Bookmark{Title: title, URL: u, Tags: cleanTags(tags), Created: s.clk.Now()})
	if err != nil {
		if ex, gerr := s.st.GetBookmarkByURL(ctx, u); gerr == nil {
			return domain.Bookmark{}, &DuplicateBookmarkError{Existing: ex}
		}
		return domain.Bookmark{}, err
	}
	b.Created = b.Created.In(s.loc)
	s.log.Info("bookmark saved", "id", b.ID, "host", hostOf(u))
	return b, nil
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return u.Host
	}
	return ""
}

// BookmarkFromClipboard saves the URL on the clipboard (first non-empty line).
func (s *Service) BookmarkFromClipboard(ctx context.Context, tags []string) (domain.Bookmark, error) {
	text, err := s.clipboard.ReadText(ctx)
	if err != nil {
		return domain.Bookmark{}, err
	}
	first := ""
	for _, ln := range strings.Split(text, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			first = ln
			break
		}
	}
	if first == "" {
		return domain.Bookmark{}, invalid("the clipboard is empty")
	}
	return s.AddBookmark(ctx, first, tags)
}

// ListBookmarks returns bookmarks, pinned first. query matches title, URL
// and tags, ignoring case; empty returns all.
func (s *Service) ListBookmarks(ctx context.Context, query string) ([]domain.Bookmark, error) {
	all, err := s.st.ListBookmarks(ctx)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := all[:0]
	for _, b := range all {
		if q == "" || strings.Contains(strings.ToLower(b.Title+" "+b.URL+" "+strings.Join(b.Tags, " ")), q) {
			b.Created = b.Created.In(s.loc)
			out = append(out, b)
		}
	}
	return out, nil
}

// PinBookmark pins or unpins a bookmark.
func (s *Service) PinBookmark(ctx context.Context, id int64, pinned bool) error {
	b, err := s.st.GetBookmark(ctx, id)
	if err != nil {
		return err
	}
	b.Pinned = pinned
	return s.st.UpdateBookmark(ctx, b)
}

// DeleteBookmark removes a bookmark.
func (s *Service) DeleteBookmark(ctx context.Context, id int64) error {
	return s.st.DeleteBookmark(ctx, id)
}

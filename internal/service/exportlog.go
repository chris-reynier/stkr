package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/daily"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/timeline"
)

const (
	// inboxKey is the export key of projects without a folder of their own.
	inboxKey   = "_daily"
	inboxFile  = "_daily/inbox.md"
	digestFile = "_daily/daily-digest.md"
	// logHistoryDays is how far back the log shows tracked time.
	logHistoryDays = 90
	maxLogFile     = 4 << 20
	maxFocusLines  = 10
)

// exportKey says which log file a project belongs to: its context id for a
// project context other than "other", inboxKey for "other", mode contexts and
// unknown projects, and "" (not exported) for personal and system contexts.
func exportKey(ctxs map[string]domain.Context, project string) string {
	c, ok := ctxs[project]
	switch {
	case !ok:
		return inboxKey
	case c.Kind == domain.KindProject && c.ID != otherContext:
		return c.ID
	case c.Kind.Exportable():
		return inboxKey
	}
	return ""
}

func logPath(key string) string {
	if key == inboxKey {
		return inboxFile
	}
	return key + "/log.md"
}

// writeExport atomically writes rel (slash separated, relative to
// onedrive_dir) and creates its folders. It refuses paths that leave
// onedrive_dir.
func (s *Service) writeExport(rel string, data []byte) error {
	root := s.cfg.OneDriveDir
	if root == "" {
		return nil
	}
	p := filepath.Join(root, filepath.FromSlash(rel))
	if r, err := filepath.Rel(root, p); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return invalid("export path %q is outside onedrive_dir", rel)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil { //nolint:gosec // the user's sync folder, not private data
		return fmt.Errorf("create export folder: %w", err)
	}
	return config.WriteFileAtomic(p, data, 0o644)
}

func (s *Service) readExport(rel string) ([]byte, error) {
	f, err := os.Open(filepath.Join(s.cfg.OneDriveDir, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxLogFile+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxLogFile {
		return nil, fmt.Errorf("%s is larger than %d MiB", rel, maxLogFile>>20)
	}
	return b, nil
}

// ExportAll rewrites the log file of every project that has items and returns
// the files written. It does nothing when onedrive_dir is empty.
func (s *Service) ExportAll(ctx context.Context) ([]string, error) {
	if s.cfg.OneDriveDir == "" {
		return nil, nil
	}
	ctxs, err := s.contextMap(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.st.ListItems(ctx, "", "")
	if err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for _, it := range items {
		keys[exportKey(ctxs, it.Project)] = true
	}
	delete(keys, "")
	return s.exportLogs(ctx, keys)
}

// exportLogs rewrites the log files for keys (see exportKey) from the
// database. Only the STKR sections change; see daily.RenderLog. Files whose
// content would not change are left untouched. Without onedrive_dir it does
// nothing. It tries every key and returns the first error with the files that
// were written.
func (s *Service) exportLogs(ctx context.Context, keys map[string]bool) ([]string, error) {
	if s.cfg.OneDriveDir == "" || len(keys) == 0 {
		return nil, nil
	}
	ctxs, err := s.contextMap(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.st.ListItems(ctx, "", "")
	if err != nil {
		return nil, err
	}
	today := timeline.Midnight(s.clk.Now(), s.loc)
	reps, err := s.dayReports(ctx, today.AddDate(0, 0, -(logHistoryDays-1)), logHistoryDays)
	if err != nil {
		return nil, err
	}
	plan, err := s.st.GetDay(ctx, today.Format(dateLayout))
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)

	var written []string
	var firstErr error
	for _, key := range names {
		lf := daily.LogFile{Title: "Inbox"}
		if c, ok := ctxs[key]; ok {
			lf.Title = c.Label
			if lf.Title == "" {
				lf.Title = c.ID
			}
		}
		byDay := map[string]*daily.LogDay{}
		day := func(date string) *daily.LogDay {
			if byDay[date] == nil {
				byDay[date] = &daily.LogDay{Date: date}
			}
			return byDay[date]
		}
		var open []domain.Item
		for _, it := range items {
			if exportKey(ctxs, it.Project) != key {
				continue
			}
			d := day(it.Date)
			d.Items = append(d.Items, daily.LogItem{Type: it.Type, Summary: it.Summary, Due: it.Due, Done: it.Status == "done"})
			if it.Status != "done" && it.Type != domain.ItemDecision && it.Type != domain.ItemNote {
				open = append(open, it)
			}
		}
		if key != inboxKey {
			for _, r := range reps {
				for _, t := range r.Totals {
					if t.ContextID == key && t.Counted > 0 {
						day(r.Date).Time += t.Counted.D()
					}
				}
			}
		}
		for _, d := range byDay {
			lf.Days = append(lf.Days, *d)
		}
		lf.Focus = focusLines(open, key == inboxKey, plan)

		rel := logPath(key)
		old, err := s.readExport(rel)
		if err == nil {
			next := daily.RenderLog(old, lf)
			if string(next) != string(old) {
				err = s.writeExport(rel, next)
				if err == nil {
					written = append(written, rel)
				}
			}
		}
		if err != nil {
			s.log.Warn("export failed", "file", rel, "err", err)
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", rel, err)
			}
		}
	}
	return written, firstErr
}

// focusLines lists the top three (inbox only) and the open actions, follow-ups
// and risks, most urgent first.
func focusLines(open []domain.Item, inbox bool, plan domain.Day) []string {
	var out []string
	if inbox {
		for i, t := range plan.Top {
			if t != "" {
				out = append(out, fmt.Sprintf("Top %d (%s): %s", i+1, plan.Date, t))
			}
		}
	}
	sort.SliceStable(open, func(i, j int) bool {
		a, b := open[i], open[j]
		if (a.Due == "") != (b.Due == "") {
			return a.Due != ""
		}
		return a.Due < b.Due
	})
	for i, it := range open {
		if i == maxFocusLines {
			break
		}
		out = append(out, daily.LogItem{Type: it.Type, Summary: it.Summary, Due: it.Due}.Line())
	}
	return out
}

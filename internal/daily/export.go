package daily

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
)

// Section headings owned by STKR and by the user.
const (
	HeadingFocus = "Current focus"
	HeadingLog   = "Log"
	HeadingNotes = "Notes"
)

// LogItem is one item bullet of the log.
type LogItem struct {
	Type    domain.ItemType
	Summary string
	Due     string
	Done    bool
}

// LogDay is one "### date" entry of the log.
type LogDay struct {
	Date  string // YYYY-MM-DD
	Time  time.Duration
	Items []LogItem
}

// LogFile is what STKR writes into a project log file.
type LogFile struct {
	// Title is used for the "# Title" header of a new file only.
	Title string
	// Focus holds the bullet lines of "## Current focus" (without the "- ").
	Focus []string
	// Days are the log entries; they are written newest first.
	Days []LogDay
}

type fileSection struct {
	name string // trimmed heading text, "" for the header block
	raw  []byte // including the heading line and trailing newline
}

// splitSections splits a markdown file at level-2 headings that are not
// inside a code fence. The first element is the header (name "").
func splitSections(b []byte) []fileSection {
	secs := []fileSection{{}}
	inFence := false
	var fenceMark string
	for _, line := range bytes.SplitAfter(b, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		text := strings.TrimRight(string(line), "\r\n")
		trimmed := strings.TrimSpace(text)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			mark := trimmed[:3]
			switch {
			case !inFence:
				inFence, fenceMark = true, mark
			case mark == fenceMark:
				inFence = false
			}
		}
		if !inFence && strings.HasPrefix(text, "## ") {
			secs = append(secs, fileSection{name: strings.TrimSpace(text[3:])})
		}
		last := &secs[len(secs)-1]
		last.raw = append(last.raw, line...)
	}
	return secs
}

func sectionIs(s fileSection, name string) bool {
	return strings.EqualFold(s.name, name) && s.name != ""
}

// RenderLog returns the new contents of a project log file. existing is the
// current file (nil when there is none). Only the "## Current focus" and
// "## Log" sections are rewritten; the header, the "## Notes" section and any
// other section are kept byte for byte. Missing STKR sections are added before
// "## Notes" (or at the end).
func RenderLog(existing []byte, f LogFile) []byte {
	crlf := bytes.Contains(existing, []byte("\r\n"))
	enc := func(s string) []byte {
		if crlf {
			s = strings.ReplaceAll(s, "\n", "\r\n")
		}
		return []byte(s)
	}
	focus := enc(focusSection(f))
	logSec := enc(logSection(f))

	secs := splitSections(existing)
	if len(existing) == 0 {
		title := f.Title
		if title == "" {
			title = "Log"
		}
		secs[0].raw = enc("# " + title + "\n\n")
	}
	var out [][]byte
	haveFocus, haveLog := false, false
	notes := -1
	for i, s := range secs {
		if sectionIs(s, HeadingNotes) && notes < 0 {
			notes = i
		}
	}
	emit := func(raw []byte) {
		// Keep sections separated: a section we append after must end in a newline.
		if n := len(out); n > 0 && len(out[n-1]) > 0 && out[n-1][len(out[n-1])-1] != '\n' {
			out = append(out, enc("\n"))
		}
		out = append(out, raw)
	}
	insertMissing := func() {
		if !haveFocus {
			emit(focus)
			haveFocus = true
		}
		if !haveLog {
			emit(logSec)
			haveLog = true
		}
	}
	for i, s := range secs {
		switch {
		case i == notes:
			insertMissing()
			emit(s.raw)
		case sectionIs(s, HeadingFocus) && !haveFocus:
			emit(focus)
			haveFocus = true
		case sectionIs(s, HeadingLog) && !haveLog:
			emit(logSec)
			haveLog = true
		default:
			emit(s.raw)
		}
	}
	insertMissing()
	if len(existing) == 0 {
		emit(enc("## " + HeadingNotes + "\n\n"))
	}
	return bytes.Join(out, nil)
}

func focusSection(f LogFile) string {
	var b strings.Builder
	b.WriteString("## " + HeadingFocus + "\n\n")
	if len(f.Focus) == 0 {
		b.WriteString("_Nothing open._\n")
	}
	for _, l := range f.Focus {
		b.WriteString("- " + l + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

func logSection(f LogFile) string {
	var b strings.Builder
	b.WriteString("## " + HeadingLog + "\n\n")
	days := append([]LogDay(nil), f.Days...)
	sort.SliceStable(days, func(i, j int) bool { return days[i].Date > days[j].Date })
	n := 0
	for _, d := range days {
		if d.Time <= 0 && len(d.Items) == 0 {
			continue
		}
		n++
		b.WriteString("### " + d.Date + "\n")
		if d.Time > 0 {
			fmt.Fprintf(&b, "- Time: %s\n", domain.FormatDuration(d.Time))
		}
		for _, it := range d.Items {
			b.WriteString("- " + it.Line() + "\n")
		}
		b.WriteString("\n")
	}
	if n == 0 {
		b.WriteString("_No entries yet._\n\n")
	}
	return b.String()
}

// Line formats the item as the text of a bullet.
func (it LogItem) Line() string {
	s := string(it.Type) + ": " + it.Summary
	if it.Due != "" && !it.Done {
		s += " (due " + it.Due + ")"
	}
	switch it.Type {
	case domain.ItemDecision, domain.ItemNote:
		return s
	}
	if it.Done {
		return "[x] " + s
	}
	return "[ ] " + s
}

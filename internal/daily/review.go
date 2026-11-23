package daily

import (
	"fmt"
	"strings"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
)

// ReviewRecord is one line of a stkr-review v1 block:
// date|type|context|text.
type ReviewRecord struct {
	Line    int
	Date    string // YYYY-MM-DD
	Type    domain.ItemType
	Context string
	Text    string
}

// ReviewBlock is an accepted stkr-review block. Start is the line of its
// opening fence.
type ReviewBlock struct {
	Start   int
	Records []ReviewRecord
}

// reviewHeader is the optional column header line of a review block.
const reviewHeader = "date|type|context|text"

type reviewState struct {
	start   int
	records []ReviewRecord
	// lines counts record lines (not blank, comment or header lines).
	lines int
	// problem is the first malformed line; the block is then rejected.
	problem string
}

// reviewLine reads one line of a stkr-review block. A malformed line (too
// few fields, bad date, empty text, placeholder) makes the whole block
// invalid; an unknown record type only drops that line.
func (ps *parser) reviewLine(st *reviewState, line string, lineNo int) {
	if st == nil || line == "" || strings.HasPrefix(line, "#") {
		return
	}
	if strings.EqualFold(strings.Join(strings.Fields(line), ""), reviewHeader) {
		return
	}
	st.lines++
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = cleanText(parts[i])
	}
	fail := func(why string) {
		if st.problem == "" {
			st.problem = fmt.Sprintf("line %d: %s", lineNo, why)
		}
	}
	if len(parts) < 4 {
		fail("a review record needs date|type|context|text")
		return
	}
	if _, err := time.Parse("2006-01-02", parts[0]); err != nil {
		fail("the date must be YYYY-MM-DD")
		return
	}
	text := truncate(strings.Join(parts[3:], " | "), MaxSummaryRunes)
	if text == "" || placeholder(text) || placeholder(parts[2]) || placeholder(parts[1]) {
		fail("the text is empty or a placeholder")
		return
	}
	typ := domain.ItemType(strings.ToLower(parts[1]))
	if !typ.Valid() {
		ps.p.Ignored++
		ps.warn("line %d: unknown review record type, line ignored", lineNo)
		return
	}
	st.records = append(st.records, ReviewRecord{Line: lineNo, Date: parts[0], Type: typ, Context: parts[2], Text: text})
}

// endReview accepts or rejects a finished review block. An unclosed block may
// be a cut-off copy, so it is rejected.
func (ps *parser) endReview(st *reviewState, closed bool) {
	if !closed && st.problem == "" {
		st.problem = "the block was not closed"
	}
	if st.problem != "" {
		ps.p.BlocksRejected++
		ps.p.Ignored += st.lines
		ps.warn("line %d: review block rejected, nothing from it was imported (%s)", st.start, st.problem)
		return
	}
	if len(st.records) == 0 {
		ps.warn("line %d: review block has no records", st.start)
		return
	}
	ps.p.Reviews = append(ps.p.Reviews, ReviewBlock{Start: st.start, Records: st.records})
}

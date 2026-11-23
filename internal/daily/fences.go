// Package daily holds the pure parts of the daily loop: the import grammar,
// digest and prompt building, prompt templates and the project log files. It
// does no I/O except for reading and writing prompt templates.
package daily

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/chris-reynier/stkr/internal/domain"
)

// Limits on imported text.
const (
	MaxSummaryRunes = 500
	MaxTopRunes     = 200
	maxWarnings     = 20
)

// Format tells how the records were found.
type Format string

// Import formats.
const (
	FormatFenced Format = "fenced"
	FormatPipe   Format = "pipe"
)

// Top is a ranked priority for the day.
type Top struct {
	Rank int
	Text string
}

// Item is a parsed item record.
type Item struct {
	Type    domain.ItemType
	Project string
	Summary string
	Due     string
}

// Parsed is the outcome of Parse.
type Parsed struct {
	// Format is fenced when at least one stkr block was found, else pipe.
	Format Format
	// Brief and Close tell which kinds of block contributed records.
	Brief, Close bool
	Tops         []Top
	Items        []Item
	// Done holds fingerprints or summaries of items to mark done.
	Done []string
	// Warnings describe lines or blocks that were ignored or accepted with
	// caveats. They never contain the text of the line.
	Warnings []string
	// Ignored counts record lines that were not usable.
	Ignored int
	// Reviews are the accepted stkr-review blocks, in text order. A block
	// with a malformed line is rejected as a whole (see BlocksRejected).
	Reviews []ReviewBlock
	// BlocksRejected counts stkr-review blocks dropped as a whole.
	BlocksRejected int
}

// Valid reports whether at least one usable record was found.
func (p Parsed) Valid() bool { return len(p.Tops)+len(p.Items)+len(p.Done)+p.ReviewRecords() > 0 }

// ReviewRecords is the number of records in the accepted review blocks.
func (p Parsed) ReviewRecords() int {
	n := 0
	for _, b := range p.Reviews {
		n += len(b.Records)
	}
	return n
}

// Fingerprint identifies an item: the first 16 hex digits of
// sha256(lower(type|project|normalized summary)).
func Fingerprint(t domain.ItemType, project, summary string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(string(t) + "|" + strings.TrimSpace(project) + "|" + NormalizeSummary(summary))))
	return hex.EncodeToString(sum[:])[:16]
}

// NormalizeSummary lower-cases s, collapses white space and drops trailing
// full stops, so small formatting differences do not create duplicates.
func NormalizeSummary(s string) string {
	s = strings.ToLower(strings.Join(strings.Fields(s), " "))
	return strings.TrimRight(s, ".! ")
}

type blockKind int

const (
	blockNone blockKind = iota
	blockBrief
	blockClose
	blockReview
)

type parser struct {
	p        Parsed
	warnings int
}

func (ps *parser) warn(format string, a ...any) {
	ps.warnings++
	if ps.warnings <= maxWarnings {
		ps.p.Warnings = append(ps.p.Warnings, fmt.Sprintf(format, a...))
	}
}

// Parse extracts import records from text. It looks for fenced blocks whose
// info string is "stkr-brief v1", "stkr-close v1" or "stkr-review v1" (see
// review.go). Without any such block,
// plain pipe-separated lines are accepted with a warning. Parse never fails;
// an unusable input yields a Parsed that is not Valid.
func Parse(text string) Parsed {
	text = strings.TrimPrefix(text, string(rune(0xFEFF)))
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(text, "\n")

	ps := &parser{p: Parsed{Format: FormatFenced}}
	blocks := 0

	var (
		inFence   bool
		fenceCh   byte
		fenceLen  int
		fenceKind = blockNone
	)
	var review *reviewState
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if inFence {
			if isFenceClose(line, fenceCh, fenceLen) {
				inFence = false
				if review != nil {
					ps.endReview(review, true)
					review = nil
				}
				continue
			}
			switch fenceKind {
			case blockReview:
				ps.reviewLine(review, line, i+1)
			case blockNone:
			default:
				ps.record(line, fenceKind, i+1)
			}
			continue
		}
		ch, n, info, ok := fenceOpen(line)
		if !ok {
			continue
		}
		inFence, fenceCh, fenceLen, fenceKind = true, ch, n, blockNone
		fields := strings.Fields(info)
		if len(fields) == 0 {
			continue
		}
		var kind blockKind
		switch strings.ToLower(fields[0]) {
		case "stkr-brief":
			kind = blockBrief
		case "stkr-close":
			kind = blockClose
		case "stkr-review":
			kind = blockReview
		default:
			continue
		}
		blocks++
		version := "v1"
		if len(fields) > 1 {
			version = strings.ToLower(fields[1])
		} else {
			ps.warn("line %d: block has no version, treated as v1", i+1)
		}
		if version != "v1" {
			ps.warn("line %d: unsupported block version, block ignored", i+1)
			continue
		}
		fenceKind = kind
		if kind == blockReview {
			review = &reviewState{start: i + 1}
		}
	}
	if inFence && fenceKind != blockNone {
		ps.warn("a stkr block was not closed; read to the end of the text")
	}
	if review != nil {
		ps.endReview(review, false)
	}

	if blocks == 0 {
		ps.p.Format = FormatPipe
		ps.pipeFallback(lines)
	}
	if !ps.p.Valid() {
		// A block that produced nothing must not mark the day saved.
		ps.p.Brief, ps.p.Close = false, false
	}
	if ps.warnings > maxWarnings {
		ps.p.Warnings = append(ps.p.Warnings, fmt.Sprintf("...and %d more warnings", ps.warnings-maxWarnings))
	}
	return ps.p
}

func fenceOpen(line string) (ch byte, n int, info string, ok bool) {
	if len(line) < 3 || (line[0] != '`' && line[0] != '~') {
		return 0, 0, "", false
	}
	ch = line[0]
	for n < len(line) && line[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0, "", false
	}
	return ch, n, strings.TrimSpace(line[n:]), true
}

func isFenceClose(line string, ch byte, n int) bool {
	if len(line) < n {
		return false
	}
	for i := 0; i < len(line); i++ {
		if line[i] != ch {
			return false
		}
	}
	return true
}

// pipeFallback reads unfenced record lines, optionally preceded by a list or
// quote marker. Lines that merely start with another word are prose.
func (ps *parser) pipeFallback(lines []string) {
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		line = strings.TrimSpace(strings.TrimLeft(line, "-*>"))
		kw, _, found := strings.Cut(line, "|")
		if !found {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(kw)) {
		case "top", "item", "done":
		default:
			continue
		}
		ps.record(line, blockNone, i+1)
	}
	if n := len(ps.p.Tops) + len(ps.p.Items) + len(ps.p.Done); n > 0 {
		ps.warn("no stkr block found: accepted %d unfenced line(s)", n)
	}
	// Infer the kind: done lines belong to a close, top lines to a brief.
	switch {
	case len(ps.p.Done) > 0:
		ps.p.Close = true
	case len(ps.p.Tops) > 0:
		ps.p.Brief = true
	}
}

// mark notes which kind of block produced a record.
func (ps *parser) mark(kind blockKind) {
	switch kind {
	case blockBrief:
		ps.p.Brief = true
	case blockClose:
		ps.p.Close = true
	case blockNone:
	}
}

// record parses one record line. kind is blockNone in the pipe fallback,
// where every record type is allowed.
func (ps *parser) record(line string, kind blockKind, lineNo int) {
	if line == "" || strings.HasPrefix(line, "#") {
		return
	}
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = cleanText(parts[i])
	}
	bad := func(why string) {
		ps.p.Ignored++
		ps.warn("line %d: %s", lineNo, why)
	}
	switch strings.ToLower(parts[0]) {
	case "top":
		if len(parts) < 3 {
			bad("top needs a rank and text")
			return
		}
		rank, err := strconv.Atoi(parts[1])
		text := truncate(strings.Join(parts[2:], " | "), MaxTopRunes)
		switch {
		case err != nil || rank < 1 || rank > 3:
			bad("top rank must be 1, 2 or 3")
		case text == "" || placeholder(text):
			bad("top text is empty")
		default:
			ps.p.Tops = append(ps.p.Tops, Top{Rank: rank, Text: text})
			ps.mark(kind)
		}
	case "item":
		if len(parts) < 4 {
			bad("item needs type, project, summary and due")
			return
		}
		typ := domain.ItemType(strings.ToLower(parts[1]))
		if !typ.Valid() {
			bad("unknown item type")
			return
		}
		project := parts[2]
		rest := parts[3:]
		due := ""
		if len(rest) >= 2 && validDue(rest[len(rest)-1]) {
			due = rest[len(rest)-1]
			rest = rest[:len(rest)-1]
		}
		summary := truncate(strings.Join(rest, " | "), MaxSummaryRunes)
		if summary == "" || placeholder(summary) || placeholder(project) {
			bad("item summary is empty")
			return
		}
		ps.p.Items = append(ps.p.Items, Item{Type: typ, Project: project, Summary: summary, Due: due})
		ps.mark(kind)
	case "done":
		if kind == blockBrief {
			bad("done lines are only used in stkr-close blocks")
			return
		}
		ref := truncate(strings.Join(parts[1:], " | "), MaxSummaryRunes)
		if ref == "" || placeholder(ref) {
			bad("done needs a fingerprint or summary")
			return
		}
		ps.p.Done = append(ps.p.Done, ref)
		ps.mark(kind)
	default:
		bad("unknown record type")
	}
}

// validDue accepts an empty due date or a real YYYY-MM-DD date.
func validDue(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// placeholder recognises template placeholders such as "<summary>", so an
// unanswered prompt left on the clipboard imports nothing.
func placeholder(s string) bool {
	return len(s) >= 2 && strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">")
}

// cleanText trims s and replaces control characters with spaces.
func cleanText(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

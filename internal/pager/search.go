package pager

import (
	"fmt"
	"regexp"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
)

// The sequences that mark a hit: reverse video for every hit, and underline as
// well for the hit in hand. Both are attributes rather than colours, so a mark
// composes with the document's own styling and reads on any theme: what the
// reader sees is their terminal's foreground and background swapped.
//
// They are also attributes md's own output does not lean on. Rendering md's
// sample document turns up bold, underline and strikethrough, and no reverse
// video and no italics at all; bold would be the usual way to pick out the hit in
// hand, but a bold hit would end in a bold-off, and that would take the bold off
// the rest of the heading it happens to sit in.
const (
	markOn, markOff       = "\x1b[7m", "\x1b[27m" // reverse video
	currentOn, currentOff = "\x1b[4m", "\x1b[24m" // underline, for the hit in hand
)

// caret is a reverse-video cell, which marks where typing goes without asking the
// terminal for a cursor position.
const caret = markOn + " " + markOff

// match is one occurrence of the query: the line it is on, counting from zero,
// and the byte range it covers in that line.
//
// The offsets are into the line as it is stored, escape sequences and all,
// because the same numbers have to scroll to the hit and mark it where it is.
type match struct {
	line       int
	start, end int
}

// search is the pager's search: the query, where its hits are, and which hit the
// reader is looking at.
type search struct {
	query   string
	matches []match
	// current indexes matches, or is -1 when the reader is not on a hit.
	current int
	// notice is what the status bar has to say about the last search - "not
	// found", "search wrapped" - until the next key is pressed.
	notice string
}

// run searches content for query and moves to the first hit at or after the line
// the reader is looking at.
//
// A search that finds nothing, or that has to go back to the top to find
// something, says so: the reader can see the document did not move, and needs to
// know which of the two happened.
func (s *search) run(content, query string, from int) {
	*s = search{query: query, current: -1}
	if query == "" {
		return
	}
	s.matches = findMatches(content, query)
	if len(s.matches) == 0 {
		s.notice = "not found: " + query
		return
	}
	for i, m := range s.matches {
		if m.line >= from {
			s.current = i
			return
		}
	}
	s.current = 0
	s.notice = "search wrapped"
}

// rescan finds the query again in content that has been rendered afresh, which a
// resize causes: the document re-flows, so the hits move with it. Where the
// reader is among them is settled by nearest, once the scroll position has been
// restored.
func (s *search) rescan(content string) {
	if s.query == "" {
		return
	}
	s.matches = findMatches(content, s.query)
	s.current = -1
	if len(s.matches) > 0 {
		s.current = 0
	}
}

// nearest puts the reader on the last hit at or before a line, which is the hit
// they were looking at before the document re-flowed.
func (s *search) nearest(line int) {
	if len(s.matches) == 0 {
		return
	}
	s.current = 0
	for i, m := range s.matches {
		if m.line > line {
			break
		}
		s.current = i
	}
}

// next and previous step through the hits, wrapping at either end and saying so.
// Wrapping rather than stopping is what less does, and it means n always has
// somewhere to go.
func (s *search) next() {
	if len(s.matches) == 0 {
		return
	}
	s.current = (s.current + 1) % len(s.matches)
	if s.current == 0 {
		s.notice = "search wrapped"
	}
}

func (s *search) previous() {
	if len(s.matches) == 0 {
		return
	}
	if s.current <= 0 {
		s.notice = "search wrapped"
	}
	s.current = (s.current - 1 + len(s.matches)) % len(s.matches)
}

// active reports whether there is a search the reader can step through.
func (s *search) active() bool { return s.current >= 0 && len(s.matches) > 0 }

// line returns the line of the hit in hand, or -1 when there is none.
func (s *search) line() int {
	if !s.active() {
		return -1
	}
	return s.matches[s.current].line
}

// count returns "3/12" for the status bar: which hit the reader is on, and how
// many the search found. It is empty when no search has found anything.
func (s *search) count() string {
	if !s.active() {
		return ""
	}
	return fmt.Sprintf("%d/%d", s.current+1, len(s.matches))
}

// painted returns content with every hit marked, and the hit in hand marked as
// such.
//
// It is always built from the plain content, so painting twice is harmless: a
// resize, a scroll and a step can each ask for a drawing of the same document
// with the marks in it, and none of them accumulates marks left by the last.
func (s *search) painted(content string) string {
	if len(s.matches) == 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	for i := range lines {
		if painted := s.paintLine(lines[i], i); painted != lines[i] {
			lines[i] = painted
		}
	}
	return strings.Join(lines, "\n")
}

// paintLine marks the hits on one line, walking from left to right so that the
// offsets of the hits still to come stay where they were.
func (s *search) paintLine(line string, n int) string {
	var b strings.Builder
	at := 0
	for i, m := range s.matches {
		if m.line != n || m.start < at || m.end > len(line) {
			continue // another line, or a hit the match finder cannot make
		}
		b.WriteString(line[at:m.start])
		mark(&b, line[m.start:m.end], i == s.current)
		at = m.end
	}
	if at == 0 {
		return line
	}
	b.WriteString(line[at:])
	return b.String()
}

// mark writes a run of a line wrapped in the mark for a hit.
//
// The mark is put back on after any escape sequence inside the run. Those escapes
// are the document's own styling, and they end with a reset: a reset would take
// the mark off again half way through a hit that straddles two style runs, leaving
// the second half of the hit unmarked.
func mark(b *strings.Builder, run string, current bool) {
	on := func() {
		b.WriteString(markOn)
		if current {
			b.WriteString(currentOn)
		}
	}
	off := func() {
		if current {
			b.WriteString(currentOff)
		}
		b.WriteString(markOff)
	}

	on()
	state := byte(xansi.NormalState)
	for i := 0; i < len(run); {
		seq, width, n, next := xansi.DecodeSequence(run[i:], state, nil)
		if n <= 0 {
			break
		}
		state = next
		if width == 0 {
			// An escape sequence: let it do its work, then mark again.
			off()
			b.WriteString(seq)
			on()
		} else {
			b.WriteString(seq)
		}
		i += n
	}
	off()
}

// findMatches finds every occurrence of query in content, ignoring case.
//
// The query is matched as a string rather than as a pattern, because that is what
// someone typing into a pager means: a search for "a.b" finds "a.b". It is
// matched against the text as displayed rather than as stored, so a query for
// "38" does not find the middle of a colour sequence.
func findMatches(content, query string) []match {
	if query == "" {
		return nil
	}
	// (?i) folds case over the whole pattern, and QuoteMeta leaves nothing for it
	// to fail on.
	pattern, err := regexp.Compile("(?i)" + regexp.QuoteMeta(query))
	if err != nil {
		return nil
	}
	var out []match
	for line, text := range strings.Split(content, "\n") {
		visible, back := visibleLine(text)
		for _, hit := range pattern.FindAllStringIndex(visible, -1) {
			out = append(out, match{line: line, start: back[hit[0]], end: back[hit[1]]})
		}
	}
	return out
}

// visibleLine returns a line's visible text, and the byte each of its bytes came
// from in the line as stored.
//
// A hit has to be marked in the line as stored, escapes and all, and its position
// there cannot be worked out from the visible text alone: this table is what
// carries an offset across, so a hit that begins after three colour changes still
// lands on the right byte.
func visibleLine(line string) (string, []int) {
	var b strings.Builder
	b.Grow(len(line))
	back := make([]int, 0, len(line)+1)
	state := byte(xansi.NormalState)
	for i := 0; i < len(line); {
		seq, width, n, next := xansi.DecodeSequence(line[i:], state, nil)
		if n <= 0 {
			break
		}
		state = next
		if width > 0 {
			for j := range n {
				back = append(back, i+j)
			}
			b.WriteString(seq)
		}
		i += n
	}
	// One past the end, so that a hit reaching the end of the line has an offset
	// to finish at.
	back = append(back, len(line))
	return b.String(), back
}

package pager

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
)

// styled is a document shaped the way md renders one: every line carries colour
// changes and every span ends with a reset. That is what the search offsets have
// to survive - a hit has to be marked in the line as stored, escapes and all, and
// its position there cannot be read off the visible text.
const styled = "\x1b[1mheading\x1b[0m\n" +
	"a plain \x1b[38;5;12mblue\x1b[0m word\n" +
	"\x1b[31mred\x1b[0m and \x1b[32mgreen\x1b[0m\n" +
	"café and 日本語のラベル\n" +
	"last line"

// marks removes the mark sequences, leaving the document's own escapes: what is
// left after painting has to be the document as it was, down to the byte.
var marks = strings.NewReplacer(markOn, "", markOff, "", currentOn, "", currentOff, "")

// hitText returns the visible text of each hit, which is what a reader would say
// had been found.
func hitText(content string, matches []match) []string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, xansi.Strip(lines[m.line][m.start:m.end]))
	}
	return out
}

// TestFindMatchesFindsVisibleText checks what a search finds: the text as it is
// displayed, matched as a string rather than as a pattern, and ignoring case. The
// hits are compared by the text they cover, which is also what proves the offsets
// came back from the visible text into the stored line in the right place.
func TestFindMatchesFindsVisibleText(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
		want  []string
	}{
		{"a word", "heading", []string{"heading"}},
		{"ignoring case", "HEADING", []string{"heading"}},
		{"across a colour change", "blue word", []string{"blue word"}},
		{"across a reset", "red and green", []string{"red and green"}},
		{"with an accent", "CAFÉ", []string{"café"}},
		{"with wide characters", "のラベル", []string{"のラベル"}},
		{"a full stop", "a.b", nil},
		{"a star", "red.*green", nil},
		{"part of a colour code", "38", nil},
		{"a bracket from a colour code", "[38;5", nil},
		{"a colour code's numbers", "5;12", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := hitText(styled, findMatches(styled, tc.query))
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("searching %q found %q, want %q", tc.query, got, tc.want)
			}
		})
	}
}

// TestFindMatchesCountsHitsInOrder checks the hits a step walks through, including
// two on one line.
func TestFindMatchesCountsHitsInOrder(t *testing.T) {
	const content = "one two one\nnothing here\none"
	got := findMatches(content, "one")
	if len(got) != 3 {
		t.Fatalf("found %d hits, want 3", len(got))
	}
	want := []match{{line: 0, start: 0, end: 3}, {line: 0, start: 8, end: 11}, {line: 2, start: 0, end: 3}}
	for i, m := range got {
		if m != want[i] {
			t.Errorf("hit %d = %+v, want %+v", i, m, want[i])
		}
	}
}

// TestVisibleLineMapsEveryByte is the mapping the whole search rests on: every
// byte of the visible text points at the byte it came from in the line as stored,
// and the map runs forwards so that a byte range keeps its order.
func TestVisibleLineMapsEveryByte(t *testing.T) {
	for _, line := range strings.Split(styled, "\n") {
		visible, back := visibleLine(line)
		if visible != xansi.Strip(line) {
			t.Errorf("visible text of %q = %q, want %q", line, visible, xansi.Strip(line))
		}
		if len(back) != len(visible)+1 {
			t.Fatalf("map has %d entries for %d bytes", len(back), len(visible))
		}
		if back[len(visible)] != len(line) {
			t.Errorf("map ends at %d, want the line's %d bytes", back[len(visible)], len(line))
		}
		for i := range visible {
			if line[back[i]] != visible[i] {
				t.Errorf("byte %d of the visible text came from %d, which holds %q not %q",
					i, back[i], line[back[i]], visible[i])
			}
			if i > 0 && back[i] <= back[i-1] {
				t.Errorf("map runs backwards at %d: %d after %d", i, back[i], back[i-1])
			}
		}
	}
}

// TestPaintedLeavesTheDocument checks the property that keeps painting safe: the
// marks are the only difference between the painted document and the plain one, so
// nothing is dropped, reordered or duplicated, and painting again changes nothing.
func TestPaintedLeavesTheDocument(t *testing.T) {
	var s search
	s.run(styled, "and", 0)
	painted := s.painted(styled)

	if got := marks.Replace(painted); got != styled {
		t.Errorf("painting changed the document:\n%q\n%q", got, styled)
	}
	if got := xansi.Strip(painted); got != xansi.Strip(styled) {
		t.Errorf("painting changed the visible text:\n%q\n%q", got, xansi.Strip(styled))
	}
	if again := s.painted(styled); again != painted {
		t.Error("painting the same document twice gave two answers")
	}
	if !strings.Contains(painted, markOn) {
		t.Error("the hit was not marked")
	}
}

// TestPaintMarksAStyleChangeAllTheWayAcross is the reason the mark is put back on
// after every escape inside a hit: the document's own spans end in a reset, and a
// reset takes the mark off again, so a hit straddling two spans would come out
// half marked.
func TestPaintMarksAStyleChangeAllTheWayAcross(t *testing.T) {
	var s search
	s.run(styled, "red and green", 0)
	painted := s.painted(styled)

	from := strings.Index(painted, markOn)
	to := strings.LastIndex(painted, markOff)
	if from < 0 || to < from {
		t.Fatalf("no marked run in %q", painted)
	}
	if got, want := xansi.Strip(painted[from:to]), "red and green"; got != want {
		t.Errorf("the marked run covers %q, want %q", got, want)
	}
	if !strings.Contains(painted, "\x1b[0m"+markOn) {
		t.Error("the mark was not put back on after the reset inside the hit")
	}
}

// TestPaintMarksTheHitInHand checks that the reader can tell which hit they are
// on: only that one carries the second mark as well.
func TestPaintMarksTheHitInHand(t *testing.T) {
	const content = "one\ntwo\none"
	var s search
	s.run(content, "one", 0)
	first := s.painted(content)
	if got := strings.Count(first, markOn+currentOn); got != 1 {
		t.Errorf("the hit in hand is marked %d times, want once", got)
	}

	s.next()
	second := s.painted(content)
	if got := strings.Count(second, markOn+currentOn); got != 1 {
		t.Errorf("after stepping, the hit in hand is marked %d times, want once", got)
	}
	if second == first {
		t.Error("stepping to the second hit drew the same document as the first")
	}
	// The mark moved with the reader, rather than being added to the old one.
	if strings.Index(second, markOn+currentOn) <= strings.Index(first, markOn+currentOn) {
		t.Error("stepping back to the first hit did not move the mark")
	}
}

// TestRunStartsWhereTheReaderIs checks that a search looks forwards from what the
// reader can see, and says so when it has to come back to the top instead.
func TestRunStartsWhereTheReaderIs(t *testing.T) {
	const content = "one\ntwo\none\nthree\none"

	var s search
	s.run(content, "one", 2)
	if got := s.line(); got != 2 {
		t.Errorf("searching from line 2 landed on line %d, want 2", got)
	}
	if s.notice != "" {
		t.Errorf("notice = %q, want nothing to say", s.notice)
	}

	s.run(content, "one", 99)
	if got := s.line(); got != 0 {
		t.Errorf("a search past the last hit landed on line %d, want 0", got)
	}
	if s.notice != "search wrapped" {
		t.Errorf("notice = %q, want it to say the search wrapped", s.notice)
	}

	s.run(content, "nothing", 0)
	if s.active() {
		t.Error("a search that found nothing is being treated as a search")
	}
	if s.notice != "not found: nothing" {
		t.Errorf("notice = %q, want it to say the query was not found", s.notice)
	}

	s.run(content, "", 0)
	if s.active() || s.notice != "" || s.query != "" {
		t.Errorf("an empty query left state behind: %+v", s)
	}
}

// TestStepWrapsAtEitherEnd checks n and N, which is how the reader reaches the
// second hit and finds their way back.
func TestStepWrapsAtEitherEnd(t *testing.T) {
	const content = "one\ntwo\none\nthree\none"
	var s search
	s.run(content, "one", 0)

	s.next()
	if got := s.line(); got != 2 {
		t.Errorf("after n: line %d, want 2", got)
	}
	if s.count() != "2/3" {
		t.Errorf("count = %q, want 2/3", s.count())
	}
	s.next()
	if got := s.line(); got != 4 {
		t.Errorf("after n again: line %d, want 4", got)
	}
	s.next()
	if got := s.line(); got != 0 {
		t.Errorf("n at the last hit: line %d, want it to wrap to 0", got)
	}
	if s.notice != "search wrapped" {
		t.Errorf("notice = %q, want it to say the search wrapped", s.notice)
	}
	s.previous()
	if got := s.line(); got != 4 {
		t.Errorf("N at the first hit: line %d, want it to wrap to 4", got)
	}
}

// TestMarkAddsNothingButTheMarks is why the marks are reverse video and
// underline. Both are attributes the document does not set for its text, so the
// escapes the mark turns off are ones it turned on: marking a hit inside a bold
// heading cannot un-bold the rest of the heading, which is what a mark made of
// bold and a bold-off would do.
func TestMarkAddsNothingButTheMarks(t *testing.T) {
	// A heading as md renders one: a colour and bold in one sequence, and the
	// hit sitting inside it.
	const line = "\x1b[96;1m## Mermaid diagrams\x1b[0m"
	var s search
	s.run(line, "Mermaid", 0)
	painted := s.painted(line)

	if !strings.Contains(painted, line[:len("\x1b[96;1m## ")]) {
		t.Errorf("the document's own escapes were disturbed:\n%q\n%q", painted, line)
	}
	for _, seq := range sgrSequences(painted) {
		switch seq {
		case markOn, markOff, currentOn, currentOff, "\x1b[96;1m", "\x1b[0m":
		default:
			t.Errorf("painting added the escape %q, which the document never used", seq)
		}
	}
	if !strings.Contains(painted, markOn) {
		t.Error("the hit was not marked")
	}
}

// sgrSequences returns the colour and attribute sequences a string carries, in
// the order they appear.
func sgrSequences(s string) []string {
	var out []string
	state := byte(xansi.NormalState)
	for i := 0; i < len(s); {
		seq, width, n, next := xansi.DecodeSequence(s[i:], state, nil)
		if n <= 0 {
			break
		}
		state = next
		if width == 0 && strings.HasSuffix(seq, "m") && strings.HasPrefix(seq, "\x1b[") {
			out = append(out, seq)
		}
		i += n
	}
	return out
}

// TestRescanAndNearest covers a document that has re-flowed under a search: the
// hits are found again, and the reader is put back on the one nearest the place
// they were reading.
func TestRescanAndNearest(t *testing.T) {
	var s search
	s.run("one\ntwo\none", "one", 0)

	// The same words, re-wrapped, so the hits have moved.
	s.rescan("one\ntwo\nthree\none\nfour")
	if !s.active() {
		t.Fatal("the query was lost when the document re-flowed")
	}
	if s.count() != "1/2" {
		t.Errorf("count = %q, want 1/2", s.count())
	}
	s.nearest(3)
	if got := s.line(); got != 3 {
		t.Errorf("nearest to line 3 is line %d, want 3", got)
	}
	s.nearest(0)
	if got := s.line(); got != 0 {
		t.Errorf("nearest to line 0 is line %d, want 0", got)
	}
	s.nearest(99)
	if got := s.line(); got != 3 {
		t.Errorf("nearest to a line past the end is line %d, want the last hit", got)
	}
}

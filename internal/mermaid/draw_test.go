package mermaid

import (
	"strconv"
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
)

// TestRenderFixtures draws every fixture and checks the two properties that
// matter: a drawing comes back, and it fits the width it was given. The drawing
// itself is logged so it can be read with -v while working on it.
func TestRenderFixtures(t *testing.T) {
	for _, tc := range []struct {
		file  string
		width int
		ascii bool
	}{
		{"flowchart_td.svg", 96, false},
		{"flowchart_lr.svg", 96, false},
		{"shapes.svg", 96, false},
		{"subgraph.svg", 96, false},
		{"unicode.svg", 96, false},
		{"flowchart_td.svg", 60, true},
	} {
		t.Run(tc.file+"/"+strconv.Itoa(tc.width), func(t *testing.T) {
			d := parseFixture(t, tc.file)
			lines, ok := Render(d, Options{Width: tc.width, Ascii: tc.ascii})
			if !ok {
				t.Fatalf("could not draw %s at width %d", tc.file, tc.width)
			}
			if len(lines) == 0 {
				t.Fatal("no lines came back")
			}
			for i, line := range lines {
				if w := xansi.StringWidth(line); w > tc.width {
					t.Errorf("line %d is %d cells, limit %d", i+1, w, tc.width)
				}
				if tc.ascii && !isASCII(line) {
					t.Errorf("line %d has non-ASCII characters: %q", i+1, line)
				}
			}
			t.Logf("\n%s", strings.Join(lines, "\n"))
		})
	}
}

// TestRenderRefusesWhatWillNotFit checks the other half of the contract: a
// diagram that cannot be drawn in the space available is refused so the caller
// can show the source instead.
func TestRenderRefusesWhatWillNotFit(t *testing.T) {
	d := parseFixture(t, "flowchart_lr.svg")
	if _, ok := Render(d, Options{Width: 8}); ok {
		t.Error("want a refusal for a diagram that cannot fit eight columns")
	}
}

// TestRenderLabelsAreWhole checks that md never breaks a word to fit a box: the
// scale is chosen so that labels wrap between words.
func TestRenderLabelsAreWhole(t *testing.T) {
	d := parseFixture(t, "shapes.svg")
	lines, ok := Render(d, Options{Width: 96})
	if !ok {
		t.Fatal("could not draw")
	}
	drawn := strings.Join(lines, "\n")
	for _, label := range []string{"start", "cache", "done", "retry"} {
		if !strings.Contains(drawn, label) {
			t.Errorf("label %q came out broken:\n%s", label, drawn)
		}
	}
}

// TestRenderKeepsEveryWord is the property that matters most about a label: md
// may wrap one, but it must never lose a word. Losing one was possible, because a
// pointed box claimed more room than the drawing gave it, so the extra lines the
// label wrapped to fell outside the shape and were dropped - a node labelled
// "run" came out reading "n".
func TestRenderKeepsEveryWord(t *testing.T) {
	// A wide glyph is drawn with a blank cell after it, which is what keeps the
	// line measuring straight; comparing on the run of glyphs ignores it, and
	// keeps the check to one row at a time so a word split across two rows still
	// shows up.
	runes := strings.NewReplacer(" ", "", "\u3000", "")
	for _, file := range []string{
		"flowchart_td.svg", "flowchart_lr.svg", "shapes.svg", "subgraph.svg", "unicode.svg",
	} {
		d := parseFixture(t, file)
		lines, ok := Render(d, Options{Width: 96})
		if !ok {
			t.Fatalf("%s: could not draw", file)
		}
		for i := range lines {
			lines[i] = runes.Replace(lines[i])
		}
		for _, n := range d.Nodes {
			label := strings.Join(n.Label, " ")
			for _, word := range strings.Fields(label) {
				found := false
				for _, line := range lines {
					if strings.Contains(line, word) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s: node %q lost the word %q:\n%s", file, label, word, strings.Join(lines, "\n"))
				}
			}
		}
	}
}

// TestPointedBoxKeepsItsLabelInside checks the promise the fit test makes to the
// drawing: a label is wrapped to a width that some row of the box really has, and
// is only put on rows that have room for it. The two halves used to be worked out
// separately, so they could disagree.
func TestPointedBoxKeepsItsLabelInside(t *testing.T) {
	label := []string{"container", "orchestration", "pipeline"}
	for _, size := range [][2]int{{7, 3}, {9, 5}, {11, 5}, {15, 9}, {21, 11}, {23, 11}, {27, 15}, {40, 20}} {
		w, h := size[0], size[1]
		n := Node{Shape: Diamond, Label: label}
		lines, room, top, ok := nodeLabel(n, w, h)
		if !ok {
			continue // no room at this size: the fit test's business, not the drawing's
		}
		rows := diamondRows(w, h)
		if len(rows) == 0 || len(rows) > h {
			t.Fatalf("%dx%d: %d rows", w, h, len(rows))
		}
		h = len(rows) // a pointed box is drawn at an odd size
		if top < 1 || top+len(lines) > len(rows)-1 {
			t.Errorf("%dx%d: label on rows %d..%d of %d, which covers an apex",
				w, h, top, top+len(lines)-1, len(rows))
			continue
		}
		for i, line := range lines {
			width := xansi.StringWidth(line)
			if got := rows[top+i].room(); width > got {
				t.Errorf("%dx%d: %q is %d cells on a row with room for %d", w, h, line, width, got)
			}
			if got := rows[top+i].room(); room > got {
				t.Errorf("%dx%d: wrapped to %d cells, but row %d offers %d", w, h, room, top+i, got)
			}
		}
		// The line, centred on its own row, has to stay inside that row's run.
		for i, line := range lines {
			row := rows[top+i]
			left := row.left + row.tread + 1 + (row.room()-xansi.StringWidth(line))/2
			right := left + xansi.StringWidth(line) - 1
			if left < row.left || right > row.right {
				t.Errorf("%dx%d: %q would be drawn at %d..%d, outside the row's %d..%d",
					w, h, line, left, right, row.left, row.right)
			}
		}
	}
}

// TestPointedBoxIsDrawnPointed checks the outline itself: it comes to a point at
// top and bottom, and is at its widest across its middle, which is what makes a
// decision read as a decision.
func TestPointedBoxIsDrawnPointed(t *testing.T) {
	d := Diagram{
		Width: 40, Height: 40,
		Nodes: []Node{{
			Box:   Box{X: 0, Y: 0, W: 40, H: 40},
			Shape: Diamond,
			Label: []string{"yes?"},
		}},
	}
	lines, ok := Render(d, Options{Width: 60})
	if !ok {
		t.Fatal("could not draw a pointed box")
	}
	var trimmed []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			trimmed = append(trimmed, line)
		}
	}
	if len(trimmed) < 5 {
		t.Fatalf("a pointed box came out %d rows tall:\n%s", len(trimmed), strings.Join(lines, "\n"))
	}
	if got := strings.TrimSpace(trimmed[0]); got != "/" {
		t.Errorf("top row is %q, want a single /", got)
	}
	if got := strings.TrimSpace(trimmed[len(trimmed)-1]); got != `\` {
		t.Errorf("bottom row is %q, want a single \\", got)
	}
	widths := make([]int, len(trimmed))
	widest := 0
	for i, line := range trimmed {
		widths[i] = xansi.StringWidth(strings.TrimSpace(line))
		if widths[i] > widths[widest] {
			widest = i
		}
	}
	if want := len(trimmed) / 2; widest != want {
		t.Errorf("widest row is %d of %d, want the middle row %d:\n%s",
			widest, len(trimmed), want, strings.Join(lines, "\n"))
	}
	for i := 1; i <= widest; i++ {
		if widths[i] <= widths[i-1] {
			t.Errorf("row %d is %d cells, no wider than the %d above it:\n%s",
				i, widths[i], widths[i-1], strings.Join(lines, "\n"))
			break
		}
	}
	for i := widest + 1; i < len(widths); i++ {
		if widths[i] >= widths[i-1] {
			t.Errorf("row %d is %d cells, no narrower than the %d above it:\n%s",
				i, widths[i], widths[i-1], strings.Join(lines, "\n"))
			break
		}
	}
}

// TestRenderWithoutStyleIsPlain checks the default: no style function means no
// escape sequences, which is what a caller that wants plain text gets.
func TestRenderWithoutStyleIsPlain(t *testing.T) {
	d := parseFixture(t, "flowchart_td.svg")
	lines, ok := Render(d, Options{Width: 96})
	if !ok {
		t.Fatal("could not draw")
	}
	for i, line := range lines {
		if strings.ContainsRune(line, 0x1b) {
			t.Errorf("line %d contains an escape sequence: %q", i+1, line)
		}
	}
}

// TestRenderStylesRuns checks that the caller's style function colours runs, and
// that every kind a drawing uses is offered to it.
func TestRenderStylesRuns(t *testing.T) {
	d := parseFixture(t, "flowchart_td.svg")
	seen := map[Kind]int{}
	lines, ok := Render(d, Options{Width: 96, Style: func(kind Kind, text string) string {
		seen[kind]++
		return text
	}})
	if !ok {
		t.Fatal("could not draw")
	}
	for _, kind := range []Kind{KindBorder, KindText, KindEdge, KindDim} {
		if seen[kind] == 0 {
			t.Errorf("kind %d was never styled", kind)
		}
	}
	if len(lines) == 0 {
		t.Error("no lines came back")
	}
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 0x7f {
			return false
		}
	}
	return true
}

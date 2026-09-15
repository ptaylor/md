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

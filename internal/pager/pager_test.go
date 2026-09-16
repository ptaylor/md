package pager

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/pftylr/md/internal/theme"
)

// document builds content that is far taller than any test terminal. Each line
// records the width it was rendered at, so a re-render after a resize is
// visible in the output.
func document(width int) string {
	var b strings.Builder
	for i := range 300 {
		fmt.Fprintf(&b, "w%d line %d\n", width, i)
	}
	return b.String()
}

// testOptions returns options for a pager that records the widths it is asked
// to render at.
func testOptions(renders *[]int) Options {
	return Options{
		Title:   "doc.md",
		Content: document(80),
		Width:   80,
		Height:  24,
		Palette: theme.Mono(true),
		Render: func(width int) (string, error) {
			if renders != nil {
				*renders = append(*renders, width)
			}
			return document(width), nil
		},
	}
}

// press sends a key message to the model and returns the command it produced.
func press(t *testing.T, m *model, key tea.KeyPressMsg) tea.Cmd {
	t.Helper()
	_, cmd := m.Update(key)
	return cmd
}

func TestViewFillsTheTerminal(t *testing.T) {
	m := newModel(testOptions(nil))
	view := m.View()

	if !view.AltScreen {
		t.Error("the pager should use the alternate screen")
	}
	if view.MouseMode != tea.MouseModeCellMotion {
		t.Error("the pager should report mouse events so the wheel scrolls")
	}
	if view.WindowTitle != "doc.md" {
		t.Errorf("window title = %q, want %q", view.WindowTitle, "doc.md")
	}

	lines := strings.Split(xansi.Strip(view.Content), "\n")
	if len(lines) != 24 {
		t.Errorf("view has %d lines, want the full 24-row terminal", len(lines))
	}
	if !strings.Contains(xansi.Strip(view.Content), "doc.md") {
		t.Error("status bar does not show the document title")
	}
	if !strings.Contains(xansi.Strip(view.Content), "%") {
		t.Error("status bar does not show the reading position")
	}
}

func TestScrollKeys(t *testing.T) {
	m := newModel(testOptions(nil))
	if !m.vp.AtTop() {
		t.Fatal("expected to start at the top")
	}

	press(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := m.vp.YOffset(); got != 1 {
		t.Errorf("after j: offset = %d, want 1", got)
	}
	press(t, m, tea.KeyPressMsg{Code: 'k', Text: "k"})
	if got := m.vp.YOffset(); got != 0 {
		t.Errorf("after k: offset = %d, want 0", got)
	}

	press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.vp.YOffset(); got != 1 {
		t.Errorf("after down arrow: offset = %d, want 1", got)
	}

	// space and b are the classic pager keys for a page at a time.
	page := m.vp.Height()
	press(t, m, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if got := m.vp.YOffset(); got != 1+page {
		t.Errorf("after space: offset = %d, want %d", got, 1+page)
	}
	press(t, m, tea.KeyPressMsg{Code: 'b', Text: "b"})
	if got := m.vp.YOffset(); got != 1 {
		t.Errorf("after b: offset = %d, want 1", got)
	}

	// Half-page and end-of-file keys.
	press(t, m, tea.KeyPressMsg{Code: 'd', Text: "d"})
	if got := m.vp.YOffset(); got <= 1 {
		t.Errorf("after d: offset = %d, expected to move down by half a page", got)
	}
	press(t, m, tea.KeyPressMsg{Code: 'G', Text: "G"})
	if !m.vp.AtBottom() {
		t.Error("after G: expected to be at the bottom")
	}
	press(t, m, tea.KeyPressMsg{Code: 'g', Text: "g"})
	if !m.vp.AtTop() {
		t.Error("after g: expected to be back at the top")
	}
}

// TestReturnScrollsALine covers the key most readers press first: return advances
// a line, the way it does in less. The viewport's own key map leaves it unbound,
// so without this the key did nothing at all.
func TestReturnScrollsALine(t *testing.T) {
	m := newModel(testOptions(nil))

	press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.vp.YOffset(); got != 1 {
		t.Errorf("after enter: offset = %d, want 1", got)
	}

	// A line feed is what a terminal sends for return once it has translated it
	// into a newline, so ctrl+j has to do the same.
	press(t, m, tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	if got := m.vp.YOffset(); got != 2 {
		t.Errorf("after ctrl+j: offset = %d, want 2", got)
	}

	// At the end of the document there is nowhere to go, and pressing it must
	// not scroll past the end.
	press(t, m, tea.KeyPressMsg{Code: 'G', Text: "G"})
	press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got, want := m.vp.YOffset(), m.vp.TotalLineCount()-m.vp.Height(); got != want {
		t.Errorf("after enter at the end: offset = %d, want %d", got, want)
	}
}

func TestQuitKeys(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{
		{Code: 'q', Text: "q"},
		{Code: tea.KeyEscape},
		{Code: 'c', Mod: tea.ModCtrl},
	} {
		m := newModel(testOptions(nil))
		cmd := press(t, m, key)
		if cmd == nil {
			t.Fatalf("key %q did not quit", key.String())
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("key %q produced %T, want tea.QuitMsg", key.String(), cmd())
		}
	}
}

func TestHelpLineToggles(t *testing.T) {
	m := newModel(testOptions(nil))
	rows := func() int { return len(strings.Split(m.body(), "\n")) }

	// The help line takes a row from the viewport rather than growing the view,
	// so the pager always fills exactly the terminal height.
	if got := rows(); got != m.height {
		t.Fatalf("view has %d rows, want %d", got, m.height)
	}
	bodyRows := m.vp.Height()

	press(t, m, tea.KeyPressMsg{Code: '?', Text: "?"})
	if got := rows(); got != m.height {
		t.Errorf("with help shown the view has %d rows, want %d", got, m.height)
	}
	if got := m.vp.Height(); got != bodyRows-1 {
		t.Errorf("with help shown the viewport is %d rows, want %d", got, bodyRows-1)
	}
	if !strings.Contains(xansi.Strip(m.body()), "quit") {
		t.Error("the help line does not list the quit key")
	}

	press(t, m, tea.KeyPressMsg{Code: '?', Text: "?"})
	if got := m.vp.Height(); got != bodyRows {
		t.Errorf("after hiding help the viewport is %d rows, want %d", got, bodyRows)
	}
	if strings.Contains(xansi.Strip(m.statusBar()), "quit") {
		t.Error("the help line is still present after toggling it off")
	}
}

func TestResizeRerendersAtTheNewWidth(t *testing.T) {
	var renders []int
	m := newModel(testOptions(&renders))

	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})

	if len(renders) == 0 {
		t.Fatal("resizing did not re-render the document")
	}
	if last := renders[len(renders)-1]; last != 120 {
		t.Errorf("re-rendered at width %d, want 120", last)
	}
	content := xansi.Strip(m.vp.View())
	if !strings.Contains(content, "w120 ") {
		t.Error("the view still shows content rendered at the old width")
	}
	lines := strings.Split(xansi.Strip(m.body()), "\n")
	if len(lines) != 30 {
		t.Errorf("after resize the view has %d lines, want 30", len(lines))
	}
}

// TestResizeKeepsReadingPosition checks that re-flowing does not throw the
// reader back to the top of the document.
func TestResizeKeepsReadingPosition(t *testing.T) {
	m := newModel(testOptions(nil))
	press(t, m, tea.KeyPressMsg{Code: 'G', Text: "G"})
	if !m.vp.AtBottom() {
		t.Fatal("expected to be at the bottom")
	}

	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	if m.vp.AtTop() {
		t.Error("resizing jumped back to the top of the document")
	}
	if pct := m.vp.ScrollPercent(); pct < 0.9 {
		t.Errorf("reading position dropped to %.0f%%, expected to stay near the end", pct*100)
	}
}

// TestRenderErrorsAreSurfaced checks that a failing re-render shows the problem
// rather than an empty screen.
func TestRenderErrorsAreSurfaced(t *testing.T) {
	opts := testOptions(nil)
	opts.Render = func(int) (string, error) { return "", fmt.Errorf("boom") }
	m := newModel(opts)

	_, _ = m.Update(tea.WindowSizeMsg{Width: 90, Height: 24})

	if !strings.Contains(xansi.Strip(m.body()), "boom") {
		t.Error("a failed re-render should be reported in the status area")
	}
}

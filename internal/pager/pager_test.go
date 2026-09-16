package pager

import (
	"errors"
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

// numbered returns a document of n lines whose every word is different, so that a
// search for one of them has exactly one hit on a line known in advance.
func numbered(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "line %02d\n", i)
	}
	return b.String()
}

// searchOptions returns a pager over a given document, small enough that a
// search's effect on the scroll position can be read off directly. Re-rendering
// hands back the same document, so that a resize exercises the way a search is
// carried across a re-render without the lines moving.
func searchOptions(content string) Options {
	return Options{
		Title:   "doc.md",
		Content: content,
		Width:   40,
		Height:  6,
		Palette: theme.Mono(true),
		Render:  func(int) (string, error) { return content, nil },
	}
}

// spotted returns a thirty line document in which the given lines say "spot" and
// the rest say something else, so that stepping between hits also moves the view.
func spotted(at ...int) string {
	want := make(map[int]bool, len(at))
	for _, line := range at {
		want[line] = true
	}
	var b strings.Builder
	for i := range 30 {
		if want[i] {
			b.WriteString("spot\n")
			continue
		}
		fmt.Fprintf(&b, "line %02d\n", i)
	}
	return b.String()
}

// query types a search the way a reader does: the prompt, the query, and return.
func query(t *testing.T, m *model, text string) {
	t.Helper()
	press(t, m, tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range text {
		press(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
}

// TestSearchPromptTakesTheKeys checks that a query is typed rather than obeyed:
// while the prompt is open, q and g belong to the query, and nothing moves.
func TestSearchPromptTakesTheKeys(t *testing.T) {
	m := newModel(searchOptions(numbered(30)))
	press(t, m, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	scrolled := m.vp.YOffset()
	if scrolled == 0 {
		t.Fatal("space did not scroll, so nothing is being protected")
	}

	press(t, m, tea.KeyPressMsg{Code: '/', Text: "/"})
	if !m.prompt {
		t.Fatal("the prompt did not open")
	}
	for _, key := range []tea.KeyPressMsg{
		{Code: 'q', Text: "q"},
		{Code: 'g', Text: "g"},
		{Code: 'G', Text: "G"},
		{Code: tea.KeySpace, Text: " "},
	} {
		if cmd := press(t, m, key); cmd != nil {
			t.Errorf("key %q did something other than type: %T", key.String(), cmd)
		}
	}
	if m.input != "qgG " {
		t.Errorf("typed query = %q, want %q", m.input, "qgG ")
	}
	if got := m.vp.YOffset(); got != scrolled {
		t.Errorf("the view moved while typing: %d, want %d", got, scrolled)
	}
	if got := xansi.Strip(m.body()); !strings.Contains(got, "/qgG") {
		t.Errorf("the prompt does not show what was typed:\n%s", got)
	}

	press(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.input != "qgG" {
		t.Errorf("after backspace: %q, want %q", m.input, "qgG")
	}

	// Escape leaves the prompt without searching, and without quitting.
	if cmd := press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape}); cmd != nil {
		t.Errorf("escape at the prompt produced %T, want nothing", cmd)
	}
	if m.prompt {
		t.Error("escape left the prompt open")
	}
	if m.search.query != "" {
		t.Errorf("escape searched for %q, want no search", m.search.query)
	}
}

// TestSearchFindsAndReveals is the whole point of the feature: a query finds a hit
// and the reader is shown it.
func TestSearchFindsAndReveals(t *testing.T) {
	m := newModel(searchOptions(numbered(30)))
	query(t, m, "line 12")

	if got := m.vp.YOffset(); got != 12 {
		t.Errorf("the hit is at line 12, but the view is at %d", got)
	}
	if !strings.Contains(m.vp.GetContent(), markOn) {
		t.Error("the hit was not marked in the document")
	}
	status := xansi.Strip(m.statusBar())
	if !strings.Contains(status, "/line 12") {
		t.Errorf("the status bar does not show the query:\n%s", status)
	}
	if !strings.Contains(status, "1/1") {
		t.Errorf("the status bar does not show where the reader is in the hits:\n%s", status)
	}

	// An empty query is a way of calling the search off.
	press(t, m, tea.KeyPressMsg{Code: '/', Text: "/"})
	press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if strings.Contains(m.vp.GetContent(), markOn) {
		t.Error("an empty query left marks behind")
	}
	if strings.Contains(xansi.Strip(m.statusBar()), "/line 12") {
		t.Error("an empty query left the last one showing")
	}
}

// TestSearchIgnoresCase checks the case-insensitivity the reader asked for.
func TestSearchIgnoresCase(t *testing.T) {
	m := newModel(searchOptions(numbered(30)))
	query(t, m, "LINE 12")
	if got := m.vp.YOffset(); got != 12 {
		t.Errorf("a case-insensitive search landed at %d, want 12", got)
	}
}

// TestSearchStepsThroughHits covers n and N, including what happens at the ends:
// the search wraps and says so, rather than stopping without a word.
func TestSearchStepsThroughHits(t *testing.T) {
	m := newModel(searchOptions(spotted(0, 10, 20)))
	query(t, m, "spot")
	if got := m.vp.YOffset(); got != 0 {
		t.Fatalf("the first hit is at line 0, but the view is at %d", got)
	}

	press(t, m, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if got := m.vp.YOffset(); got != 10 {
		t.Errorf("after n: line %d, want 10", got)
	}
	press(t, m, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if got := m.vp.YOffset(); got != 20 {
		t.Errorf("after n again: line %d, want 20", got)
	}
	press(t, m, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if got := m.vp.YOffset(); got != 0 {
		t.Errorf("n at the last hit: line %d, want it to wrap to 0", got)
	}
	if got := xansi.Strip(m.statusBar()); !strings.Contains(got, "search wrapped") {
		t.Errorf("wrapping was not mentioned:\n%s", got)
	}
	press(t, m, tea.KeyPressMsg{Code: 'N', Text: "N"})
	if got := m.vp.YOffset(); got != 20 {
		t.Errorf("N at the first hit: line %d, want it to wrap to 20", got)
	}

	// A notice belongs to the keypress that caused it.
	press(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := xansi.Strip(m.statusBar()); strings.Contains(got, "search wrapped") {
		t.Errorf("the notice outlived the next keypress:\n%s", got)
	}
}

// TestSearchWithoutAHit checks what a reader sees when the words are not there:
// nothing moves, nothing is marked, and the pager says why. The query is short
// because the status bar has one line to say all this in, and a long one is
// truncated there like the title beside it.
func TestSearchWithoutAHit(t *testing.T) {
	m := newModel(searchOptions(numbered(30)))
	press(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	at := m.vp.YOffset()

	query(t, m, "zzz")

	if got := m.vp.YOffset(); got != at {
		t.Errorf("the view moved to %d for a search that found nothing, want %d", got, at)
	}
	if strings.Contains(m.vp.GetContent(), markOn) {
		t.Error("a search that found nothing marked something")
	}
	if got := xansi.Strip(m.statusBar()); !strings.Contains(got, "not found: zzz") {
		t.Errorf("the status bar does not say the query was not found:\n%s", got)
	}
	// And stepping has nowhere to go.
	press(t, m, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if got := m.vp.YOffset(); got != at {
		t.Errorf("n with no hits moved the view to %d, want %d", got, at)
	}
}

// TestSearchWrapsFromTheEnd checks the reader at the end of a document, where a
// forward search has to come back to the top to find anything.
func TestSearchWrapsFromTheEnd(t *testing.T) {
	m := newModel(searchOptions(numbered(30)))
	press(t, m, tea.KeyPressMsg{Code: 'G', Text: "G"})

	query(t, m, "line 01")

	if got := m.vp.YOffset(); got != 1 {
		t.Errorf("the search landed at %d, want the hit on line 1", got)
	}
	if got := xansi.Strip(m.statusBar()); !strings.Contains(got, "search wrapped") {
		t.Errorf("the wrap was not mentioned:\n%s", got)
	}
}

// TestSearchPromptTakesARow checks that the prompt takes a row of the chrome
// rather than growing the view, the way the help line does.
func TestSearchPromptTakesARow(t *testing.T) {
	m := newModel(searchOptions(numbered(30)))
	rows := func() int { return len(strings.Split(m.body(), "\n")) }
	body := m.vp.Height()

	press(t, m, tea.KeyPressMsg{Code: '/', Text: "/"})

	if got := rows(); got != m.height {
		t.Errorf("the view has %d rows, want %d", got, m.height)
	}
	if got := m.vp.Height(); got != body-1 {
		t.Errorf("with the prompt open the viewport is %d rows, want %d", got, body-1)
	}
}

// TestResizeKeepsTheSearch checks that a re-render does not quietly drop either
// the query or its marks: the document is re-flowed, so both have to be worked out
// again from the new one.
func TestResizeKeepsTheSearch(t *testing.T) {
	m := newModel(searchOptions(numbered(30)))
	query(t, m, "line 12")

	_, _ = m.Update(tea.WindowSizeMsg{Width: 30, Height: 12})

	if !m.search.active() {
		t.Error("the search was lost when the document was re-rendered")
	}
	if !strings.Contains(m.vp.GetContent(), markOn) {
		t.Error("the marks were lost when the document was re-rendered")
	}
	if got := xansi.Strip(m.statusBar()); !strings.Contains(got, "/line 12") {
		t.Errorf("the status bar lost the query:\n%s", got)
	}
}

// TestHelpLineFitsAnOrdinaryTerminal checks that nothing is hidden by default: the
// help line is truncated to the terminal, so a key that does not fit the width of
// an ordinary terminal is a key nobody knows about.
func TestHelpLineFitsAnOrdinaryTerminal(t *testing.T) {
	for _, keys := range []string{helpKeys, helpKeysBack} {
		if got := xansi.StringWidth(keys); got > 80 {
			t.Errorf("the help line is %d cells, which does not fit an eighty column terminal: %q", got, keys)
		}
	}
	for _, want := range []string{"/", "search", "n/N", "q "} {
		if !strings.Contains(helpKeys, want) {
			t.Errorf("the help line does not mention %q: %q", want, helpKeys)
		}
	}
}

// TestQuittingWithBackLeavesTheDocumentToList checks the way out of a document
// that was chosen from a list: the keys that quit ask for the list back, and say
// so in the help line, rather than ending md.
func TestQuittingWithBackLeavesTheDocumentToList(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{
		{Code: 'q', Text: "q"},
		{Code: tea.KeyEscape},
		{Code: 'c', Mod: tea.ModCtrl},
	} {
		o := testOptions(nil)
		o.Back = true
		m := newModel(o)
		if got := m.outcome(); got != nil {
			t.Fatalf("before any key: outcome = %v, want nil", got)
		}
		press(t, m, key)
		if got := m.outcome(); !errors.Is(got, ErrBack) {
			t.Errorf("key %q: outcome = %v, want ErrBack", key.String(), got)
		}
		if got := xansi.Strip(m.helpLine()); !strings.Contains(got, "q back") {
			t.Errorf("key %q: the help line does not offer the way back:\n%s", key.String(), got)
		}
	}

	// And without Back, quitting means quitting.
	m := newModel(testOptions(nil))
	press(t, m, tea.KeyPressMsg{Code: 'q', Text: "q"})
	if got := m.outcome(); got != nil {
		t.Errorf("quitting an ordinary document: outcome = %v, want nil", got)
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

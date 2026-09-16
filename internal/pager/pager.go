// Package pager shows a rendered document in a full-screen, scrollable
// viewport with pager-style key bindings, or hands it to an external pager.
package pager

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/pftylr/md/internal/theme"
)

// Options configure the pager.
type Options struct {
	// Title is shown in the status bar, usually the file name.
	Title string
	// Content is the rendered document.
	Content string
	// Width and Height are the initial terminal size.
	Width, Height int
	// Palette colours the chrome.
	Palette theme.Palette
	// Render re-renders the document at a new width, so that resizing the
	// terminal re-flows the text instead of leaving it wrapped for the old
	// width.
	Render func(width int) (string, error)
}

// Show runs the pager until the user quits.
func Show(o Options) error {
	if _, err := tea.NewProgram(newModel(o)).Run(); err != nil {
		return fmt.Errorf("starting pager: %w", err)
	}
	return nil
}

// newModel builds the initial pager state. It is separate from Show so that the
// pager can be driven directly in tests, without a terminal.
func newModel(o Options) *model {
	m := &model{
		opts:     o,
		width:    o.Width,
		height:   o.Height,
		content:  o.Content,
		vp:       viewport.New(),
		rendered: o.Width,
	}
	m.vp.SoftWrap = true // long code lines are wrapped rather than clipped
	m.resize()
	return m
}

type model struct {
	opts Options
	vp   viewport.Model
	// content is the document as rendered, without the search marks; the marked
	// document is built from it, so the marks can never accumulate.
	content  string
	width    int
	height   int
	rendered int // the width the content was rendered at
	err      error
	showHelp bool
	// prompt is open while the reader is typing a query, and input is what they
	// have typed so far.
	prompt bool
	input  string
	search search
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil

	case tea.KeyPressMsg:
		// A notice belongs to the keypress that caused it: the reader has read
		// it by the time they press anything else.
		m.search.notice = ""
		if m.prompt {
			return m.promptKey(msg)
		}
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "?":
			m.showHelp = !m.showHelp
			m.resize()
			return m, nil
		case "g", "home":
			m.vp.GotoTop()
			return m, nil
		case "G", "end":
			m.vp.GotoBottom()
			return m, nil
		case "/":
			m.prompt = true
			m.input = ""
			m.resize()
			return m, nil
		case "n":
			m.step(1)
			return m, nil
		case "N":
			m.step(-1)
			return m, nil
		case "enter", "ctrl+j":
			// Return advances a line: the first key anyone presses in a pager,
			// and the one the viewport's key map leaves unbound. A line feed is
			// the same key to a terminal that has translated return into
			// newline, so ctrl+j comes with it, as it does in less.
			m.vp.ScrollDown(1)
			return m, nil
		}
	}

	// Everything else (j/k, space/b, d/u, arrows, page keys, mouse wheel) is
	// the viewport's own default key map, which is deliberately pager-like.
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

// promptKey handles a key while the search prompt is open. Every printable key
// belongs to the query, so the pager's own keys - q, ?, g, j and the rest - wait
// until the reader leaves the prompt: typing a query containing a q must not quit.
func (m *model) promptKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "ctrl+c":
		m.prompt = false
		m.resize()
	case "enter":
		m.prompt = false
		m.search.run(m.content, m.input, m.vp.YOffset())
		m.resize() // the prompt's row goes back to the document
		m.show()
	case "backspace":
		m.input = dropLast(m.input)
	default:
		if key.Text != "" {
			m.input += key.Text
		}
	}
	return m, nil
}

// step moves to the next or previous hit, and shows it.
func (m *model) step(delta int) {
	if !m.search.active() {
		return
	}
	if delta < 0 {
		m.search.previous()
	} else {
		m.search.next()
	}
	m.show()
}

// show marks the hits in the document and reveals the hit in hand.
//
// The hit goes to the top line of the screen, so that what follows it is what the
// reader reads next; near the end of the document it goes as far as the document
// allows, because the viewport clamps the offset.
func (m *model) show() {
	m.vp.SetContent(m.search.painted(m.content))
	if line := m.search.line(); line >= 0 {
		m.vp.SetYOffset(line)
	}
}

// dropLast removes the last character of the query, which is what backspace means
// while the prompt is open.
func dropLast(s string) string {
	if s == "" {
		return ""
	}
	_, size := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-size]
}

func (m *model) View() tea.View {
	v := tea.NewView(m.body())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = m.opts.Title
	return v
}

// resize re-lays out the viewport and re-renders the document if the width
// changed.
func (m *model) resize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	m.vp.SetWidth(m.width)
	m.vp.SetHeight(max(m.height-m.chromeHeight(), 1))
	if m.width != m.rendered {
		m.rerender()
	}
	m.vp.SetContent(m.search.painted(m.content))
}

// rerender re-renders at the current width, keeping the reader roughly where
// they were in the document.
func (m *model) rerender() {
	if m.opts.Render == nil {
		m.rendered = m.width
		return
	}
	at := m.vp.ScrollPercent()
	out, err := m.opts.Render(m.width)
	if err != nil {
		m.err = err
		return
	}
	m.err = nil
	m.rendered = m.width
	m.content = out
	// The document has re-flowed, so the hits have moved with it: find the query
	// again, put the reader back where they were, and let the hit nearest that
	// place be the one in hand.
	m.search.rescan(out)
	m.vp.SetContent(out)
	m.vp.SetYOffset(int(at * float64(m.vp.TotalLineCount())))
	m.search.nearest(m.vp.YOffset())
}

func (m *model) chromeHeight() int {
	height := 1 // the status bar
	if m.showHelp {
		height++
	}
	if m.prompt {
		height++
	}
	return height
}

func (m *model) body() string {
	if m.err != nil {
		return m.statusStyle().Width(m.width).Render(" md: " + m.err.Error())
	}
	rows := []string{m.vp.View()}
	if m.showHelp {
		rows = append(rows, m.helpLine())
	}
	if m.prompt {
		rows = append(rows, m.promptLine())
	}
	rows = append(rows, m.statusBar())
	return strings.Join(rows, "\n")
}

// promptLine draws the search prompt: what has been typed, and a cell for the
// caret, so it is plain where the next character goes.
func (m *model) promptLine() string {
	line := "/" + m.input
	if xansi.StringWidth(line) > m.width-1 {
		// A query longer than the screen keeps its end, which is where typing
		// happens.
		line = xansi.TruncateLeft(line, m.width-2, "…")
	}
	return m.statusStyle().Width(m.width).Render(line + caret + " ")
}

// statusBar shows the file name on the left and the reading position on the
// right, as a full-width bar in the palette's panel colour. A search adds its
// state there - the query and which hit the reader is on - or, in its place, what
// went wrong with it.
func (m *model) statusBar() string {
	p := m.opts.Palette
	position := fmt.Sprintf("%3.0f%%", m.vp.ScrollPercent()*100)
	right := position
	if count := m.search.count(); count != "" {
		right = count + "  " + position
	}
	left := m.opts.Title
	switch {
	case m.search.notice != "":
		left += "  " + m.search.notice
	case m.search.query != "":
		left += "  /" + m.search.query
	}
	// Account for the spaces around both ends.
	room := m.width - xansi.StringWidth(right) - 4
	if room < 0 {
		room = 0
	}
	left = xansi.Truncate(left, room, "…")

	gap := m.width - xansi.StringWidth(left) - xansi.StringWidth(right) - 4
	if gap < 0 {
		gap = 0
	}
	inner := " " + left + strings.Repeat(" ", gap) + right + " "
	return lipgloss.NewStyle().
		Foreground(theme.Color(p.Dim())).
		Background(theme.Color(p.Panel())).
		Width(m.width).
		Render(inner)
}

// helpKeys names what the help line offers. It is a constant so that a test can
// check it fits an ordinary terminal, where the line is not truncated: a key that
// does not fit is a key nobody knows about.
const helpKeys = "  j/k/enter scroll · space/b page · d/u half · / search · n/N · ? help · q quit"

func (m *model) helpLine() string {
	return m.statusStyle().Width(m.width).Render(xansi.Truncate(helpKeys, m.width, "…"))
}

func (m *model) statusStyle() lipgloss.Style {
	p := m.opts.Palette
	return lipgloss.NewStyle().
		Foreground(theme.Color(p.Dim())).
		Background(theme.Color(p.Panel()))
}

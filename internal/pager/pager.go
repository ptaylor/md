// Package pager shows a rendered document in a full-screen, scrollable
// viewport with pager-style key bindings, or hands it to an external pager.
package pager

import (
	"fmt"
	"strings"

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
	opts     Options
	vp       viewport.Model
	content  string
	width    int
	height   int
	rendered int // the width the content was rendered at
	err      error
	showHelp bool
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil

	case tea.KeyPressMsg:
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
		}
	}

	// Everything else (j/k, space/b, d/u, arrows, page keys, mouse wheel) is
	// the viewport's own default key map, which is deliberately pager-like.
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
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
	m.vp.SetContent(m.content)
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
	m.vp.SetContent(out)
	m.vp.SetYOffset(int(at * float64(m.vp.TotalLineCount())))
}

func (m *model) chromeHeight() int {
	if m.showHelp {
		return 2
	}
	return 1
}

func (m *model) body() string {
	if m.err != nil {
		return m.statusStyle().Width(m.width).Render(" md: " + m.err.Error())
	}
	rows := []string{m.vp.View()}
	if m.showHelp {
		rows = append(rows, m.helpLine())
	}
	rows = append(rows, m.statusBar())
	return strings.Join(rows, "\n")
}

// statusBar shows the file name on the left and the reading position on the
// right, as a full-width bar in the palette's panel colour.
func (m *model) statusBar() string {
	p := m.opts.Palette
	title := m.opts.Title
	position := fmt.Sprintf("%3.0f%%", m.vp.ScrollPercent()*100)
	// Account for the spaces around both ends.
	room := m.width - len(position) - 4
	if room < 0 {
		room = 0
	}
	title = xansi.Truncate(title, room, "…")

	gap := m.width - xansi.StringWidth(title) - len(position) - 4
	if gap < 0 {
		gap = 0
	}
	inner := " " + title + strings.Repeat(" ", gap) + position + " "
	return lipgloss.NewStyle().
		Foreground(theme.Color(p.Dim())).
		Background(theme.Color(p.Panel())).
		Width(m.width).
		Render(inner)
}

func (m *model) helpLine() string {
	keys := "  j/k scroll · space/b page · d/u half · g/G ends · ? help · q quit"
	return m.statusStyle().Width(m.width).Render(xansi.Truncate(keys, m.width, "…"))
}

func (m *model) statusStyle() lipgloss.Style {
	p := m.opts.Palette
	return lipgloss.NewStyle().
		Foreground(theme.Color(p.Dim())).
		Background(theme.Color(p.Panel()))
}

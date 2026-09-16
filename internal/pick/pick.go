// Package pick offers a list of files to choose from, for when md is pointed at a
// directory rather than at a document.
package pick

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/pftylr/md/internal/theme"
)

// ErrCancelled reports that the reader left the list without choosing, which is
// not a failure: there is simply nothing to show.
var ErrCancelled = errors.New("no file chosen")

// markOn and markOff are reverse video, the terminal's own foreground and
// background swapped. It is the mark a search uses, for the same reason: it reads
// on any theme, and it needs no colour of md's own.
const (
	markOn, markOff = "\x1b[7m", "\x1b[27m"
)

// Options configure the list.
type Options struct {
	// Dir is the directory being chosen from, which the status bar names.
	Dir string
	// Files are the files to offer, in the order they are shown.
	Files []string
	// Width and Height are the initial terminal size.
	Width, Height int
	// Palette colours the chrome.
	Palette theme.Palette
}

// Files returns the Markdown files in one directory, by name.
//
// Names are compared without regard to case, so that README.md and appendix.md
// sort into one alphabet rather than two, and subdirectories are not descended
// into: a directory is a place to choose in, not a tree to walk.
func Files(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() || !isMarkdown(entry.Name()) {
			continue
		}
		out = append(out, filepath.Join(dir, entry.Name()))
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(filepath.Base(out[i])) < strings.ToLower(filepath.Base(out[j]))
	})
	return out, nil
}

// isMarkdown reports whether a name is a Markdown document. The extension is
// compared without regard to case, because README.MD is a Markdown file.
func isMarkdown(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".md")
}

// Show offers the files and returns the one chosen, or ErrCancelled when the
// reader leaves without choosing.
func Show(o Options) (string, error) {
	if len(o.Files) == 0 {
		// Nothing to offer and nobody to offer it to: do not take the screen.
		return "", ErrCancelled
	}
	final, err := tea.NewProgram(newModel(o)).Run()
	if err != nil {
		return "", fmt.Errorf("starting the list: %w", err)
	}
	m, ok := final.(*model)
	if !ok || m.cancelled || m.chosen < 0 || m.chosen >= len(o.Files) {
		return "", ErrCancelled
	}
	return o.Files[m.chosen], nil
}

// newModel builds the initial list state. It is separate from Show so that the
// list can be driven directly in tests, without a terminal.
func newModel(o Options) *model {
	return &model{opts: o, width: o.Width, height: o.Height}
}

type model struct {
	opts   Options
	width  int
	height int
	top    int // the first file on screen
	chosen int // the file the reader is on
	// cancelled is set when the reader leaves without choosing.
	cancelled bool
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.reveal()

	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			return m, tea.Quit
		case "up", "k", "ctrl+p":
			m.move(-1)
		case "down", "j", "ctrl+n":
			m.move(1)
		case "home", "g":
			m.chosen = 0
			m.reveal()
		case "end", "G":
			m.chosen = len(m.opts.Files) - 1
			m.reveal()
		}
	}
	return m, nil
}

func (m *model) View() tea.View {
	v := tea.NewView(m.body())
	v.AltScreen = true
	v.WindowTitle = m.opts.Dir
	return v
}

// move steps the choice, and keeps it on screen.
func (m *model) move(delta int) {
	m.chosen += delta
	m.reveal()
}

// rows is how many files fit above the status bar.
func (m *model) rows() int { return max(m.height-1, 1) }

// reveal keeps the chosen file in view, and the choice within the list: stepping
// past either end stops there rather than wrapping, because a list is a place
// rather than a loop.
func (m *model) reveal() {
	if len(m.opts.Files) == 0 {
		return
	}
	m.chosen = min(max(m.chosen, 0), len(m.opts.Files)-1)
	m.top = min(max(m.top, 0), m.chosen)
	if last := m.top + m.rows() - 1; m.chosen > last {
		m.top = m.chosen - m.rows() + 1
	}
}

func (m *model) body() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	rows := make([]string, 0, m.height)
	for i := m.top; i < len(m.opts.Files) && len(rows) < m.rows(); i++ {
		rows = append(rows, m.row(i))
	}
	for len(rows) < m.rows() {
		rows = append(rows, strings.Repeat(" ", m.width))
	}
	return strings.Join(append(rows, m.statusBar()), "\n")
}

// row draws one file, the reader's choice in reverse video and the rest plain,
// every one of them the full width so that the mark reads as a bar across the
// list.
func (m *model) row(i int) string {
	line := padTo(" "+filepath.Base(m.opts.Files[i]), m.width)
	if i == m.chosen {
		return markOn + line + markOff
	}
	return line
}

// statusBar names the directory and which file the reader is on, with the keys
// beside it: a list has no room to hide its help behind a key, and does not need
// to.
func (m *model) statusBar() string {
	left := m.opts.Dir
	if n := len(m.opts.Files); n > 0 {
		left += fmt.Sprintf("  %d/%d", m.chosen+1, n)
	}
	right := "↑↓ choose · enter open · q quit"
	p := m.opts.Palette
	style := lipgloss.NewStyle().
		Foreground(theme.Color(p.Dim())).
		Background(theme.Color(p.Panel()))

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
	return style.Width(m.width).Render(" " + left + strings.Repeat(" ", gap) + right + " ")
}

// padTo extends a line to the full width, or trims it to fit, so that a long file
// name cannot push the frame out of shape.
func padTo(line string, width int) string {
	if width <= 0 {
		return ""
	}
	if gap := width - xansi.StringWidth(line); gap > 0 {
		return line + strings.Repeat(" ", gap)
	}
	return xansi.Truncate(line, width, "…")
}

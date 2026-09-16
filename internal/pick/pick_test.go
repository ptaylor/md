package pick

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/pftylr/md/internal/theme"
)

// tree builds a directory holding the given files, creating a directory for any
// name that ends in a slash.
func tree(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		path := filepath.Join(dir, name)
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(path, []byte("# "+name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestFilesOffersMarkdownOnly checks what the list offers: the Markdown files of
// the one directory, in an order a reader can find things in, whatever the case
// of the extension and whatever else is lying about.
func TestFilesOffersMarkdownOnly(t *testing.T) {
	dir := tree(t, "notes.md", "README.MD", "appendix.md", "image.png", "script.sh", "deep/")
	if err := os.WriteFile(filepath.Join(dir, "deep", "buried.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	files, err := Files(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(files))
	for i, file := range files {
		got[i] = filepath.Base(file)
	}
	want := []string{"appendix.md", "notes.md", "README.MD"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("offered %v, want %v", got, want)
	}
}

// TestFilesReportsAMissingDirectory checks the other half of the contract: a
// directory that is not there is an error to report, not an empty list to show.
func TestFilesReportsAMissingDirectory(t *testing.T) {
	if _, err := Files(filepath.Join(t.TempDir(), "gone")); err == nil {
		t.Error("reading a directory that is not there reported no error")
	}
}

// TestShowWithNothingToOffer checks that an empty list does not take the screen.
func TestShowWithNothingToOffer(t *testing.T) {
	if _, err := Show(Options{Dir: "somewhere"}); !errors.Is(err, ErrCancelled) {
		t.Errorf("Show with no files = %v, want ErrCancelled", err)
	}
}

// listOptions offers n files in a terminal of a given size.
func listOptions(n, width, height int) Options {
	files := make([]string, n)
	for i := range n {
		files[i] = filepath.Join("docs", fmt.Sprintf("file%02d.md", i))
	}
	return Options{Dir: "docs", Files: files, Width: width, Height: height, Palette: theme.Mono(true)}
}

// press sends a key to the list.
func press(t *testing.T, m *model, key tea.KeyPressMsg) tea.Cmd {
	t.Helper()
	_, cmd := m.Update(key)
	return cmd
}

func name(m *model) string { return filepath.Base(m.opts.Files[m.chosen]) }

// TestListStepsAndStops checks the keys a reader will use first, and what happens
// at either end of the list: it stops, rather than wrapping somewhere unexpected.
func TestListStepsAndStops(t *testing.T) {
	m := newModel(listOptions(5, 30, 6))

	press(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if got := name(m); got != "file02.md" {
		t.Errorf("after two steps down: %s, want file02.md", got)
	}
	press(t, m, tea.KeyPressMsg{Code: 'k', Text: "k"})
	if got := name(m); got != "file01.md" {
		t.Errorf("after a step up: %s, want file01.md", got)
	}
	press(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	press(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := name(m); got != "file00.md" {
		t.Errorf("stepping up past the first file: %s, want file00.md", got)
	}
	press(t, m, tea.KeyPressMsg{Code: 'G', Text: "G"})
	if got := name(m); got != "file04.md" {
		t.Errorf("after G: %s, want the last file", got)
	}
	press(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := name(m); got != "file04.md" {
		t.Errorf("stepping down past the last file: %s, want it to stay", got)
	}
	press(t, m, tea.KeyPressMsg{Code: 'g', Text: "g"})
	if got := name(m); got != "file00.md" {
		t.Errorf("after g: %s, want the first file", got)
	}
}

// TestListRevealsTheChoice checks the property a long list depends on: whatever
// the reader is on is on screen, so that a list taller than the terminal can
// still be walked.
func TestListRevealsTheChoice(t *testing.T) {
	m := newModel(listOptions(40, 30, 6))
	body := func() string { return xansi.Strip(m.body()) }

	press(t, m, tea.KeyPressMsg{Code: 'G', Text: "G"})
	if !strings.Contains(body(), "file39.md") {
		t.Errorf("the last file is not on screen:\n%s", body())
	}
	if m.top == 0 {
		t.Error("a list of forty files in a six row terminal did not scroll")
	}

	// And back up: the first file has to come into view again.
	press(t, m, tea.KeyPressMsg{Code: 'g', Text: "g"})
	if !strings.Contains(body(), "file00.md") {
		t.Errorf("the first file is not on screen:\n%s", body())
	}
	if m.top != 0 {
		t.Errorf("the list is scrolled to %d with the first file chosen, want 0", m.top)
	}
}

// TestListFillsTheTerminal checks that the list is exactly as tall as the
// terminal, with the status bar at the bottom, so that nothing is drawn over and
// nothing is left ragged.
func TestListFillsTheTerminal(t *testing.T) {
	for _, height := range []int{4, 6, 24} {
		m := newModel(listOptions(40, 40, height))
		rows := strings.Split(m.body(), "\n")
		if len(rows) != height {
			t.Errorf("height %d: the list is %d rows", height, len(rows))
		}
		for i, row := range rows {
			if got := xansi.StringWidth(row); got != 40 {
				t.Errorf("height %d: row %d is %d cells, want 40", height, i, got)
			}
		}
	}

	// A list shorter than the terminal still fills it.
	m := newModel(listOptions(2, 40, 6))
	if rows := strings.Split(m.body(), "\n"); len(rows) != 6 {
		t.Errorf("a short list is %d rows, want 6", len(rows))
	}
}

// TestListMarksTheChoice checks that the reader can see which file they are on,
// and that the mark moves with them.
func TestListMarksTheChoice(t *testing.T) {
	m := newModel(listOptions(5, 30, 6))
	if got := strings.Count(m.body(), markOn); got != 1 {
		t.Fatalf("the chosen file is marked %d times, want once", got)
	}
	first := m.body()

	press(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	second := m.body()
	if first == second {
		t.Error("stepping did not change what is drawn")
	}
	if !strings.Contains(second, markOn+" file01.md") {
		t.Errorf("the mark is not on the file the reader is on:\n%q", second)
	}
}

// TestListCancelsAndChooses covers the two ways out: q leaves with nothing, enter
// leaves with the file the reader is on.
func TestListCancelsAndChooses(t *testing.T) {
	m := newModel(listOptions(5, 30, 6))
	cmd := press(t, m, tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("q did not end the list")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("q produced %T, want tea.QuitMsg", cmd())
	}
	if !m.cancelled {
		t.Error("q did not record that the reader left without choosing")
	}

	m = newModel(listOptions(5, 30, 6))
	press(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	cmd = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter did not end the list")
	}
	if m.cancelled {
		t.Error("enter recorded the choice as a cancellation")
	}
	if got := name(m); got != "file01.md" {
		t.Errorf("enter chose %s, want file01.md", got)
	}
}

// TestListTrimsLongNames checks that a name wider than the terminal cannot push
// the list out of shape.
func TestListTrimsLongNames(t *testing.T) {
	o := listOptions(3, 24, 5)
	o.Files[1] = filepath.Join("docs", strings.Repeat("very-long-name", 6)+".md")
	m := newModel(o)

	for i, row := range strings.Split(m.body(), "\n") {
		if got := xansi.StringWidth(row); got != 24 {
			t.Errorf("row %d is %d cells, want 24: %q", i, got, row)
		}
	}
}

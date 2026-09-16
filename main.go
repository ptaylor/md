// Command md renders a Markdown file in the terminal.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	xterm "github.com/charmbracelet/x/term"

	"github.com/pftylr/md/internal/cache"
	"github.com/pftylr/md/internal/config"
	mdmermaid "github.com/pftylr/md/internal/mermaid"
	"github.com/pftylr/md/internal/pager"
	"github.com/pftylr/md/internal/pick"
	"github.com/pftylr/md/internal/render"
	"github.com/pftylr/md/internal/term"
)

// version is stamped by install.sh from the repository's git description.
var version = "dev"

const usageText = `md - render Markdown in the terminal

usage:
  md [options] FILE.md
  md [options] DIRECTORY    choose a Markdown file from one
  md [options] -            read from standard input

options:
  -w, --width N        render width in columns (default: terminal width)
      --max-width N    cap the content width (default: 100)
      --theme NAME     auto, dark, light or mono (default: auto)
      --pager NAME     auto, builtin, less or none (default: auto)
      --mermaid NAME   auto, box or off (default: auto)
      --links NAME     auto, inline, both or plain (default: auto)
      --ascii          use ASCII glyphs instead of box drawing characters
      --no-color       disable colour (also honours NO_COLOR)
      --color          force colour even when output is not a terminal
      --no-probe       do not query the terminal for its palette
      --refresh-palette    ignore the cached palette and query again
      --clear-cache        remove the cached palette and diagrams
      --version        print the version and exit
  -h, --help           print this help

keys (built-in pager):
  j/k, up/down         line up and down        space/b, pgdn/pgup   page down and up
  d/u, ctrl-d/ctrl-u   half page down and up   g/G, home/end        top and bottom
  q, esc, ctrl-c       quit                    ?                    toggle help
  /                    search                  n/N                  next/previous hit

keys (a directory):
  up/down, j/k         choose                  enter                read the file
  q, esc, ctrl-c       leave the list

With a directory and no terminal to choose on - piped, or with --pager=none - md
lists the Markdown files it found instead of asking.

md matches the colours you already use: it reads the terminal's own 16 colour
slots and background, and falls back to referring to those slots by index on
terminals that do not answer queries.
`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "md:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("md", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usageText) }

	var (
		width      = cfg.Width
		maxWidth   = cfg.MaxWidth
		themeName  = cfg.Theme
		pagerMode  = cfg.Pager
		mermaid    = cfg.Mermaid
		links      = cfg.Links
		ascii      = cfg.Ascii
		noColor    = cfg.NoColor
		noProbe    = cfg.NoProbe
		forceCol   = false
		refresh    = false
		clearCache = false
		showVer    = false
	)
	fs.IntVar(&width, "width", width, "render width in columns")
	fs.IntVar(&width, "w", width, "render width in columns")
	fs.IntVar(&maxWidth, "max-width", maxWidth, "cap the content width")
	fs.StringVar(&themeName, "theme", themeName, "auto, dark, light or mono")
	fs.StringVar(&pagerMode, "pager", pagerMode, "auto, builtin, less or none")
	fs.StringVar(&mermaid, "mermaid", mermaid, "auto, box or off")
	fs.StringVar(&links, "links", links, "auto, inline, both or plain")
	fs.BoolVar(&ascii, "ascii", ascii, "use ASCII glyphs")
	fs.BoolVar(&noColor, "no-color", noColor, "disable colour")
	fs.BoolVar(&forceCol, "color", false, "force colour even when not a terminal")
	fs.BoolVar(&noProbe, "no-probe", noProbe, "do not query the terminal palette")
	fs.BoolVar(&refresh, "refresh-palette", false, "ignore the cached palette")
	fs.BoolVar(&clearCache, "clear-cache", false, "remove the cached palette and diagrams")
	fs.BoolVar(&showVer, "version", false, "print the version and exit")

	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if showVer {
		fmt.Println("md", version)
		return nil
	}
	if clearCache {
		// Clearing the cache and rendering go together: the point of clearing is
		// to see the document drawn afresh, with a palette re-read and diagrams
		// rendered again.
		cleared, err := cache.Clear()
		if err != nil {
			return fmt.Errorf("clearing the cache: %w", err)
		}
		if cleared {
			fmt.Fprintf(os.Stderr, "md: cleared %s\n", cache.Dir())
		} else {
			fmt.Fprintln(os.Stderr, "md: nothing was cached")
		}
		if len(fs.Args()) == 0 && !isPiped() {
			return nil // cleared, and given nothing to render
		}
	}

	// A directory is the one argument that is not a document: md offers the
	// Markdown files in it and lets the reader choose. Its files are not read yet,
	// because the choice needs the terminal, and asking the terminal anything has
	// to wait until standard input has been drained.
	dir := directory(fs.Args())
	var name, src string
	if dir == "" {
		var err error
		if name, src, err = input(fs.Args()); err != nil {
			return err
		}
	}

	caps := term.Detect(term.Options{
		Theme:      themeName,
		NoColor:    noColor,
		NoProbe:    noProbe,
		Refresh:    refresh,
		ForceColor: forceCol,
		Width:      width,
	})
	ascii = ascii || caps.Ascii
	// Diagrams are drawn with mmdc when it is installed. When stderr is a
	// terminal the wait is reported diagram by diagram, and the summary at the
	// end would only repeat it.
	showProgress := xterm.IsTerminal(os.Stderr.Fd())
	diagrams := mermaidDiagrams(mermaid, cfg.MermaidCmd, caps, ascii, showProgress)
	if d, ok := diagrams.(*render.Diagrams); ok && !showProgress {
		defer reportDiagramFailures(d)
	}
	rend := render.New(caps, render.Options{
		MaxWidth: maxWidth,
		Mermaid:  mermaid,
		Links:    links,
		Ascii:    ascii,
		Diagrams: diagrams,
	})

	if dir != "" {
		return browse(rend, caps, pagerMode, dir)
	}

	content, err := rend.Render(src, caps.Width)
	if err != nil {
		return err
	}
	return display(caps, pagerMode, name, content, fromSource(rend, src), false)
}

// directory reports the directory md has been pointed at, if it has been.
//
// A directory is a place to choose a document in rather than a document, and it is
// the only argument that is not read as one. A path that cannot be read at all is
// left to input, which says what is wrong with it.
func directory(args []string) string {
	if len(args) != 1 || args[0] == "-" {
		return ""
	}
	if info, err := os.Stat(args[0]); err == nil && info.IsDir() {
		return args[0]
	}
	return ""
}

// browse offers the Markdown files of a directory, and shows the one chosen.
//
// Reading a document returns to the list, so that several can be read in a row,
// and leaving the list is how md ends. Nothing can be chosen without a terminal,
// or with the pager switched off: then md lists what it found, which is useful on
// its own and is not a failure.
func browse(rend *render.Renderer, caps term.Caps, mode, dir string) error {
	files, err := pick.Files(dir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no Markdown files in %s", dir)
	}
	if !caps.IsTTY || mode == "none" {
		_, err := fmt.Fprintln(caps.Out, strings.Join(files, "\n"))
		return err
	}

	for {
		chosen, err := pick.Show(pick.Options{
			Dir:     dir,
			Files:   files,
			Width:   caps.Width,
			Height:  caps.Height,
			Palette: caps.Palette,
		})
		if errors.Is(err, pick.ErrCancelled) {
			return nil
		}
		if err != nil {
			return err
		}

		src, err := os.ReadFile(chosen) //nolint:gosec // a path the reader chose
		if err != nil {
			return err
		}
		content, err := rend.Render(string(src), caps.Width)
		if err != nil {
			return err
		}
		err = display(caps, mode, filepath.Base(chosen), content, fromSource(rend, string(src)), true)
		if errors.Is(err, pager.ErrBack) {
			continue
		}
		return err
	}
}

// fromSource re-renders a document from its source, which is what resizing needs:
// what is on screen is markdown's output, not markdown.
func fromSource(rend *render.Renderer, src string) func(width int) (string, error) {
	return func(width int) (string, error) { return rend.Render(src, width) }
}

// mermaidDiagrams returns the diagram drawer for the chosen mode, or nil when
// diagrams should be shown as source.
//
// auto - the default - uses mmdc when it is installed and shows the source when
// it is not, so nothing has to be installed for md to work, and nothing has to
// be configured once it is.
func mermaidDiagrams(mode, cmd string, caps term.Caps, ascii, showProgress bool) render.Diagrammer {
	if mode == "off" || mode == "box" {
		return nil
	}
	tool := &mdmermaid.Tool{Command: cmd}
	if !tool.Available() {
		return nil
	}
	// A cold run starts a browser per diagram, which takes seconds: say so, say
	// how to skip it, and then say what is happening as each diagram is drawn.
	// All of it goes to stderr, because stdout stays a document.
	if showProgress {
		tool.OnFirstRun = func() {
			// The timings on the lines that follow say how slow this is; the
			// header only has to say what is going on and how to opt out.
			fmt.Fprintln(os.Stderr, "md: drawing diagrams with mmdc")
			fmt.Fprintln(os.Stderr, "md: --mermaid box shows the source, --mermaid off treats it as code")
		}
		tool.Progress = reportDiagramProgress
	}
	diagrams := render.NewDiagrams(tool, caps.Palette, ascii)
	if !diagrams.Available() {
		return nil
	}
	return diagrams
}

// reportDiagramProgress writes one line per diagram: as it starts, and as it
// finishes. Diagrams come back in whatever order they happen to finish, since
// they are drawn at the same time, so the position in the document is printed
// rather than implied by the order.
func reportDiagramProgress(p mdmermaid.Progress) {
	prefix := fmt.Sprintf("md: %2d/%-2d %-42s ", p.Index, p.Total, p.Label)
	switch {
	case p.Err != nil:
		// A refusal that was remembered costs nothing to report, so say that
		// rather than letting it look like work.
		remembered := ""
		if p.Cached {
			remembered = " (remembered)"
		}
		fmt.Fprintln(os.Stderr, prefix+"failed: "+firstLine(p.Err.Error())+remembered)
	case p.Cached:
		fmt.Fprintln(os.Stderr, prefix+"cached")
	case p.Started:
		fmt.Fprintln(os.Stderr, prefix+"drawing...")
	default:
		fmt.Fprintf(os.Stderr, "%sdrawn in %s\n", prefix, p.Took.Round(time.Millisecond))
	}
}

// firstLine returns the first line of a message, capped so that one long error
// does not run away across the terminal.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return capRunes(strings.TrimSpace(s), 80)
}

// capRunes trims a string to n characters, marking that it was trimmed.
func capRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// reportDiagramFailures says once what could not be drawn, because a diagram
// quietly replaced by its source is confusing when you expecting a picture.
func reportDiagramFailures(d *render.Diagrams) {
	failures := d.Failures()
	if len(failures) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "md: %d diagram(s) could not be drawn, showing source: %v\n",
		len(failures), failures[0])
}

// display shows a rendered document, with the pager when the terminal can take
// one.
//
// rerender renders the document again at a new width, for a resize. back is set
// when the document was chosen from a list, so that leaving it returns to that
// list instead of ending md - and ErrBack is passed on to the caller rather than
// treated as a pager that failed to start.
func display(caps term.Caps, mode, name, content string, rerender func(int) (string, error), back bool) error {
	if !caps.IsTTY {
		// Piped or redirected: no pager, and the writer strips colour unless
		// the user forced it.
		_, err := io.WriteString(caps.Out, content)
		return err
	}
	switch mode {
	case "none":
		_, err := io.WriteString(caps.Out, content)
		return err
	case "less":
		return pager.ShowExternal(content, name)
	}
	err := pager.Show(pager.Options{
		Title:   name,
		Content: content,
		Width:   caps.Width,
		Height:  caps.Height,
		Palette: caps.Palette,
		Render:  rerender,
		Back:    back,
	})
	if err == nil || errors.Is(err, pager.ErrBack) {
		return err
	}
	// The interactive pager could not start; fall back rather than lose the
	// document.
	fmt.Fprintln(os.Stderr, "md:", err)
	if err := pager.ShowExternal(content, name); err == nil {
		return nil
	}
	_, err = io.WriteString(caps.Out, content)
	return err
}

// input reads the document from a file, from standard input, or decides there
// is nothing to do.
func input(args []string) (name, src string, err error) {
	switch len(args) {
	case 0:
		if isPiped() {
			break
		}
		return "", "", errors.New("no input: pass a Markdown file, or '-' to read standard input")
	case 1:
		if args[0] != "-" {
			data, readErr := os.ReadFile(args[0]) //nolint:gosec
			if readErr != nil {
				return "", "", readErr
			}
			return filepath.Base(args[0]), string(data), nil
		}
	default:
		return "", "", fmt.Errorf("expected a single file, got %d arguments", len(args))
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", "", fmt.Errorf("reading standard input: %w", err)
	}
	return "<stdin>", string(data), nil
}

// isPiped reports whether standard input is a pipe or a file rather than a
// terminal, so that "md < file.md" and "cat file.md | md" work with no
// arguments at all.
func isPiped() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
}

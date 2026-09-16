// Command md renders a Markdown file in the terminal.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	xterm "github.com/charmbracelet/x/term"

	"github.com/pftylr/md/internal/config"
	mdmermaid "github.com/pftylr/md/internal/mermaid"
	"github.com/pftylr/md/internal/pager"
	"github.com/pftylr/md/internal/render"
	"github.com/pftylr/md/internal/term"
)

// version is stamped by install.sh from the repository's git description.
var version = "dev"

const usageText = `md - render Markdown in the terminal

usage:
  md [options] FILE.md
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
      --refresh-palette
                       ignore the cached palette and query again
      --version        print the version and exit
  -h, --help           print this help

keys (built-in pager):
  j/k, up/down         line up and down        space/b, pgdn/pgup   page down and up
  d/u, ctrl-d/ctrl-u   half page down and up   g/G, home/end        top and bottom
  q, esc, ctrl-c       quit                    ?                    toggle help

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
		width     = cfg.Width
		maxWidth  = cfg.MaxWidth
		themeName = cfg.Theme
		pagerMode = cfg.Pager
		mermaid   = cfg.Mermaid
		links     = cfg.Links
		ascii     = cfg.Ascii
		noColor   = cfg.NoColor
		noProbe   = cfg.NoProbe
		forceCol  = false
		refresh   = false
		showVer   = false
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

	name, src, err := input(fs.Args())
	if err != nil {
		return err
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
	diagrams := mermaidDiagrams(mermaid, cfg.MermaidCmd, caps, ascii)
	if d, ok := diagrams.(*render.Diagrams); ok {
		defer reportDiagramFailures(d)
	}
	rend := render.New(caps, render.Options{
		MaxWidth: maxWidth,
		Mermaid:  mermaid,
		Links:    links,
		Ascii:    ascii,
		Diagrams: diagrams,
	})

	content, err := rend.Render(src, caps.Width)
	if err != nil {
		return err
	}
	return display(rend, caps, pagerMode, name, content)
}

// mermaidDiagrams returns the diagram drawer for the chosen mode, or nil when
// diagrams should be shown as source.
//
// auto - the default - uses mmdc when it is installed and shows the source when
// it is not, so nothing has to be installed for md to work, and nothing has to
// be configured once it is.
func mermaidDiagrams(mode, cmd string, caps term.Caps, ascii bool) render.Diagrammer {
	if mode == "off" || mode == "box" {
		return nil
	}
	tool := &mdmermaid.Tool{Command: cmd}
	if !tool.Available() {
		return nil
	}
	// A cold run starts a browser per diagram, which takes seconds: say so, and
	// say how to skip it, on stderr - stdout stays a document.
	if xterm.IsTerminal(os.Stderr.Fd()) {
		tool.OnFirstRun = func() {
			fmt.Fprintln(os.Stderr, "md: drawing diagrams with mmdc - slow the first time, cached after that")
			fmt.Fprintln(os.Stderr, "md: pass --mermaid box (show the source) or --mermaid off (treat it as code) to skip it")
		}
	}
	diagrams := render.NewDiagrams(tool, caps.Palette, ascii)
	if !diagrams.Available() {
		return nil
	}
	return diagrams
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
func display(rend *render.Renderer, caps term.Caps, mode, name, content string) error {
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
		Render: func(width int) (string, error) {
			return rend.Render(content, width)
		},
	})
	if err == nil {
		return nil
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

// Package term discovers what the terminal can do: its size, colour profile,
// background polarity, ANSI palette and whether it renders clickable links.
package term

import (
	"os"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	xterm "github.com/charmbracelet/x/term"

	"github.com/pftylr/md/internal/theme"
)

// Options controls capability detection.
type Options struct {
	// Theme forces the background polarity: "", "dark", "light" or "mono".
	Theme string
	// NoColor disables colour entirely. NO_COLOR in the environment does the
	// same.
	NoColor bool
	// NoProbe skips querying the terminal for its own palette.
	NoProbe bool
	// Refresh ignores a cached palette.
	Refresh bool
	// ForceColor emits colour even when stdout is not a terminal.
	ForceColor bool
	// Width overrides the detected terminal width.
	Width int
}

// Caps describes the terminal md is rendering to.
type Caps struct {
	// Width and Height are the usable text area in cells.
	Width, Height int
	// Profile is how much colour the output supports.
	Profile colorprofile.Profile
	// Out writes to stdout, stripping or downsampling colour as required.
	Out *colorprofile.Writer
	// Palette is the palette to render with.
	Palette theme.Palette
	// Dark reports whether the terminal background is dark.
	Dark bool
	// OSC8 reports whether links can be made clickable.
	OSC8 bool
	// Ascii reports whether the terminal or locale cannot be trusted with
	// box drawing characters.
	Ascii bool
	// IsTTY reports whether stdout is a terminal.
	IsTTY bool
	// Probed reports whether the palette came from the terminal itself.
	Probed bool
}

// Detect inspects the environment. It never fails: every probe has a fallback
// so md renders something sensible on any terminal.
func Detect(o Options) Caps {
	c := Caps{Out: &colorprofile.Writer{Forward: os.Stdout}}
	c.IsTTY = xterm.IsTerminal(os.Stdout.Fd())
	c.Profile = colorprofile.Detect(os.Stdout, os.Environ())
	if o.ForceColor && c.Profile < colorprofile.TrueColor {
		c.Profile = colorprofile.TrueColor
	}
	c.Out.Profile = c.Profile
	c.Width, c.Height = size()
	if o.Width > 0 {
		c.Width = o.Width
	}
	c.OSC8 = osc8Supported()
	c.Ascii = asciiTerminal()

	probed := ProbeResult{}
	tty := openTTY()
	if !o.NoProbe && c.IsTTY && queriesSupported() {
		probed = QueryPalette(tty, o.Refresh)
	}
	c.Probed = probed.Complete()

	dark := resolveDark(o.Theme, probed, tty)
	if tty != nil {
		defer func() { _ = tty.Close() }()
	}
	c.Dark = dark

	switch {
	case o.NoColor || strings.EqualFold(o.Theme, "mono") || os.Getenv("NO_COLOR") != "":
		c.Palette = theme.Mono(dark)
	case probed.Answered:
		c.Palette = probed.Palette(dark)
	default:
		c.Palette = theme.Default(dark)
	}
	return c
}

// resolveDark decides whether the terminal background is dark, preferring the
// probed background colour, then the terminal's own answer to an OSC 11 query,
// and finally assuming dark.
func resolveDark(themeOpt string, probed ProbeResult, tty *os.File) bool {
	switch strings.ToLower(themeOpt) {
	case "light":
		return false
	case "dark":
		return true
	}
	if probed.Answered && probed.HasBG {
		return probed.Dark
	}
	in, out := os.Stdin, os.Stdout
	if tty != nil {
		in, out = tty, tty
	}
	return lipgloss.HasDarkBackground(in, out)
}

// asciiTerminal reports whether non-ASCII glyphs are risky: a dumb terminal or
// a non-UTF-8 locale.
func asciiTerminal() bool {
	if strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return true
	}
	locale := os.Getenv("LC_ALL")
	if locale == "" {
		locale = os.Getenv("LC_CTYPE")
	}
	if locale == "" {
		locale = os.Getenv("LANG")
	}
	if locale == "" {
		return false // macOS terminals are UTF-8 regardless of the environment
	}
	upper := strings.ToUpper(locale)
	return !strings.Contains(upper, "UTF-8") && !strings.Contains(upper, "UTF8")
}

// openTTY opens the controlling terminal, which is what md queries even when
// stdin or stdout is a pipe.
func openTTY() *os.File {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil
	}
	return f
}

// queriesSupported reports whether it is worth asking the terminal anything.
func queriesSupported() bool {
	if strings.EqualFold(os.Getenv("TERM_PROGRAM"), "Apple_Terminal") {
		return false // never answers OSC queries
	}
	switch strings.ToLower(os.Getenv("TERM")) {
	case "", "dumb", "unknown":
		return false
	}
	return true
}

// size returns the terminal size, falling back to the environment and then to
// a conventional 80x24.
func size() (int, int) {
	if w, h, err := xterm.GetSize(os.Stdout.Fd()); err == nil && w > 0 && h > 0 {
		return w, h
	}
	w, h := envInt("COLUMNS"), envInt("LINES")
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	return w, h
}

func envInt(name string) int {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil {
		return 0
	}
	return n
}

// osc8Supported reports whether the terminal renders OSC 8 hyperlinks. md hides
// a link's URL only when the link text itself is clickable, because on
// terminals without support the escape sequences are ignored and the URL would
// otherwise be lost.
func osc8Supported() bool {
	if v, ok := os.LookupEnv("MD_HYPERLINKS"); ok {
		return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
	}
	if os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("WEZTERM_PANE") != "" {
		return true
	}
	if os.Getenv("WT_SESSION") != "" || os.Getenv("VTE_VERSION") != "" {
		return true
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app", "WezTerm", "ghostty", "vscode", "Hyper", "Tabby", "rio", "alacritty":
		return true
	case "Apple_Terminal":
		return false
	}
	term := strings.ToLower(os.Getenv("TERM"))
	for _, name := range []string{"kitty", "wezterm", "ghostty", "foot"} {
		if strings.Contains(term, name) {
			return true
		}
	}
	return false
}

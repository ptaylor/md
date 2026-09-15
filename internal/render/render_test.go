package render

import (
	"fmt"
	"image/color"
	"os"
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/colorprofile"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/pftylr/md/internal/term"
	"github.com/pftylr/md/internal/theme"
)

// fixture is the document the layout tests render.
func fixture(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("testdata/fixture.md")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return string(data)
}

// newTestRenderer returns a renderer with a fixed, deterministic capability set.
// The palette is the ANSI-index tier, so runs do not depend on the machine's
// terminal.
func newTestRenderer(width int, opts Options, profile colorprofile.Profile, dark bool) *Renderer {
	if opts.MaxWidth == 0 {
		opts.MaxWidth = width
	}
	palette := theme.Default(dark)
	if profile <= colorprofile.NoTTY {
		palette = theme.Mono(dark)
	}
	caps := term.Caps{
		Width:   width,
		Height:  24,
		Dark:    dark,
		Profile: profile,
		Palette: palette,
	}
	return New(caps, opts)
}

// TestRenderedLinesFitWidth is the core layout guarantee: nothing md emits may
// exceed the terminal width, or the terminal will wrap it and break every panel.
func TestRenderedLinesFitWidth(t *testing.T) {
	src := fixture(t)
	for _, width := range []int{40, 60, 80, 100, 132, 200} {
		t.Run(widthName(width), func(t *testing.T) {
			out, err := newTestRenderer(width, Options{}, colorprofile.ANSI256, true).Render(src, width)
			if err != nil {
				t.Fatal(err)
			}
			for i, line := range strings.Split(out, "\n") {
				if got := xansi.StringWidth(line); got > width {
					t.Errorf("line %d is %d cells wide, limit is %d:\n%s",
						i+1, got, width, xansi.Strip(line))
				}
			}
		})
	}
}

// TestNoStderrNoise guards against the renderer writing anything but the
// document to its output: md must produce identical output whether or not it is
// being paged.
func TestNoStderrNoise(t *testing.T) {
	out, err := newTestRenderer(80, Options{}, colorprofile.ANSI256, true).Render(fixture(t), 80)
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"unhandled", "Warning", "warning"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("rendered output contains %q", unwanted)
		}
	}
}

func TestGolden(t *testing.T) {
	src := fixture(t)
	cases := []struct {
		name    string
		width   int
		opts    Options
		profile colorprofile.Profile
		dark    bool
	}{
		{name: "colour_80_dark", width: 80, profile: colorprofile.ANSI256, dark: true},
		{name: "colour_80_light", width: 80, profile: colorprofile.ANSI256, dark: false},
		{name: "mono_80", width: 80, profile: colorprofile.NoTTY, dark: true},
		{name: "ascii_70", width: 70, opts: Options{Ascii: true}, profile: colorprofile.NoTTY, dark: true},
		{name: "narrow_44", width: 44, profile: colorprofile.NoTTY, dark: true},
		{name: "mermaid_off_80", width: 80, opts: Options{Mermaid: "off"}, profile: colorprofile.NoTTY, dark: true},
		{name: "links_both_80", width: 80, opts: Options{Links: "both"}, profile: colorprofile.ANSI256, dark: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := newTestRenderer(tc.width, tc.opts, tc.profile, tc.dark).Render(src, tc.width)
			if err != nil {
				t.Fatal(err)
			}
			golden.RequireEqual(t, out)
		})
	}
}

func TestExtractFootnotes(t *testing.T) {
	src := "One[^a] two[^b] again[^a] and code `[^a]` and a fence:\n\n" +
		"```\n[^a]: not a definition\n```\n\n" +
		"[^a]: First note.\n    Continued here.\n\n" +
		"[^b]: Second note.\n\n[^unused]: Never referenced.\n"

	body, notes := extractFootnotes(src)

	if strings.Contains(body, "First note.") || strings.Contains(body, "Second note.") {
		t.Errorf("definitions were not removed from the body:\n%s", body)
	}
	if !strings.Contains(body, "One[1] two[2] again[1]") {
		t.Errorf("references were not numbered in reading order:\n%s", body)
	}
	if !strings.Contains(body, "`[^a]`") {
		t.Errorf("a reference inside inline code was rewritten:\n%s", body)
	}
	if !strings.Contains(body, "[^a]: not a definition") {
		t.Errorf("a definition inside a code fence was removed:\n%s", body)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes, got %d: %+v", len(notes), notes)
	}
	if notes[0].number != 1 || notes[0].text != "First note. Continued here." {
		t.Errorf("first note is wrong: %+v", notes[0])
	}
	if notes[1].number != 2 || notes[1].text != "Second note." {
		t.Errorf("second note is wrong: %+v", notes[1])
	}
}

func TestSplitFrontMatter(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		wantBody  string
		wantFront string
	}{
		{
			name:      "yaml",
			src:       "---\ntitle: x\n---\n# Heading\n",
			wantBody:  "# Heading\n",
			wantFront: "title: x",
		},
		{
			name:      "toml",
			src:       "+++\ntitle = \"x\"\n+++\n# Heading\n",
			wantBody:  "# Heading\n",
			wantFront: "title = \"x\"",
		},
		{
			name:     "unterminated is not front matter",
			src:      "---\ntitle: x\n\n# Heading\n",
			wantBody: "---\ntitle: x\n\n# Heading\n",
		},
		{
			name:     "plain document",
			src:      "# Heading\n\nText.\n",
			wantBody: "# Heading\n\nText.\n",
		},
		{
			name:      "byte order mark",
			src:       "\ufeff---\ntitle: x\n---\nbody\n",
			wantBody:  "body\n",
			wantFront: "title: x",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, front := splitFrontMatter(tc.src)
			if body != tc.wantBody {
				t.Errorf("body = %q, want %q", body, tc.wantBody)
			}
			if front != tc.wantFront {
				t.Errorf("front matter = %q, want %q", front, tc.wantFront)
			}
		})
	}
}

func TestLinkMode(t *testing.T) {
	// What glamour v2 emits for "[text](https://example.com)": the text run and
	// the URL run, each wrapped in an OSC 8 hyperlink.
	const rendered = "\x1b]8;id=1;https://example.com\x1b\\text\x1b]8;;\x1b\\" +
		" " +
		"\x1b]8;id=1;https://example.com\x1b\\https://example.com\x1b]8;;\x1b\\"

	cases := []struct {
		mode string
		osc8 bool
		want string
	}{
		{mode: "both", want: rendered},
		{mode: "inline", want: "\x1b]8;id=1;https://example.com\x1b\\text\x1b]8;;\x1b\\"},
		{mode: "plain", want: "text https://example.com"},
		{mode: "auto", osc8: false, want: rendered},
		{mode: "auto", osc8: true, want: "\x1b]8;id=1;https://example.com\x1b\\text\x1b]8;;\x1b\\"},
	}
	for _, tc := range cases {
		name := tc.mode + "_osc8_" + boolName(tc.osc8)
		t.Run(name, func(t *testing.T) {
			r := &Renderer{
				caps: term.Caps{OSC8: tc.osc8, Palette: theme.Default(true)},
				opts: Options{Links: tc.mode},
			}
			if got := r.applyLinkMode(rendered); got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

// TestPanelGeometry checks that the boxes md draws stay the same width on every
// row, which is what makes them look like boxes.
func TestPanelGeometry(t *testing.T) {
	r := newTestRenderer(100, Options{}, colorprofile.NoTTY, true)
	for _, lines := range [][]string{
		{"short"},
		{"a line that is comfortably long but still fits"},
		{""},
	} {
		for _, width := range []int{40, 60, 100} {
			out := r.panel("label", lines, docMargin, width)
			if len(out) < 3 {
				t.Fatalf("panel has %d rows", len(out))
			}
			want := xansi.StringWidth(out[0])
			for i, row := range out {
				if got := xansi.StringWidth(row); got != want {
					t.Errorf("width %d: row %d is %d cells, want %d:\n%s",
						width, i, got, want, xansi.Strip(row))
				}
			}
		}
	}
}

func TestWrapCellsKeepsContent(t *testing.T) {
	r := newTestRenderer(60, Options{}, colorprofile.NoTTY, true)
	long := strings.Repeat("ab ", 40)
	parts := r.wrapCells(long, 20)
	if len(parts) < 2 {
		t.Fatalf("expected the line to be wrapped, got %d part(s)", len(parts))
	}
	var joined strings.Builder
	for i, p := range parts {
		if got := xansi.StringWidth(p); got > 20 {
			t.Errorf("part %d is %d cells, limit is 20", i, got)
		}
		joined.WriteString(strings.TrimSpace(xansi.Strip(strings.TrimPrefix(p, "↳ "))))
	}
	if !strings.Contains(joined.String(), "ab ab ab") {
		t.Errorf("wrapping lost content: %q", joined.String())
	}
}

func widthName(w int) string { return "width_" + itoa(w) }

// TestAsciiOutputIsAscii checks that --ascii really is ASCII: glamour's own
// ASCII style still uses a bullet character, so this is easy to regress.
func TestAsciiOutputIsAscii(t *testing.T) {
	out, err := newTestRenderer(72, Options{Ascii: true}, colorprofile.ANSI256, true).Render(fixture(t), 72)
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(out, "\n") {
		for _, r := range line {
			if r > unicode.MaxASCII {
				t.Errorf("line %d contains %q, which is not ASCII: %q", i+1, r, xansi.Strip(line))
				break
			}
		}
	}
}

// newMonoRenderer renders with colour disabled, whatever the terminal is
// capable of, so that the escape-free guarantee can be asserted against a
// profile that would otherwise emit colour.
func newMonoRenderer(width int, profile colorprofile.Profile, dark bool) *Renderer {
	caps := term.Caps{
		Width:   width,
		Height:  24,
		Dark:    dark,
		Profile: profile,
		Palette: theme.Mono(dark),
	}
	return New(caps, Options{MaxWidth: width})
}

// TestMonoOutputHasNoEscapes checks that with colour disabled - through
// NO_COLOR or --theme mono - nothing is emitted that a plain text file would
// not contain. glamour adds bold attributes to headings regardless of the
// palette, and OSC 8 escapes around links, so this needs asserting.
func TestMonoOutputHasNoEscapes(t *testing.T) {
	for _, profile := range []colorprofile.Profile{colorprofile.NoTTY, colorprofile.TrueColor} {
		t.Run("profile_"+itoa(int(profile)), func(t *testing.T) {
			out, err := newMonoRenderer(80, profile, true).Render(fixture(t), 80)
			if err != nil {
				t.Fatal(err)
			}
			if i := strings.IndexByte(out, 0x1b); i >= 0 {
				near := out[max(i-40, 0):min(i+40, len(out))]
				t.Errorf("mono output contains an escape sequence near: %q", near)
			}
		})
	}
}

// TestMonoKeepsLinkURLs checks that disabling colour does not quietly drop link
// destinations: the escapes that made the link text clickable are stripped, so
// the URL text is the only thing left to show where a link points.
func TestMonoKeepsLinkURLs(t *testing.T) {
	src := "See [the docs](https://example.com/docs) for more.\n"
	out, err := newMonoRenderer(80, colorprofile.TrueColor, true).Render(src, 80)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "https://example.com/docs") {
		t.Errorf("the link URL was lost in mono mode: %q", out)
	}
}

func boolName(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// probedCaps returns capabilities of the kind a real terminal reports: exact RGB
// values for the sixteen slots, and a default foreground and background to
// match.
func probedCaps(dark bool, fg, bg color.RGBA) term.Caps {
	var slots [theme.NumSlots]color.RGBA
	for i := range slots {
		slots[i] = theme.DefaultSlot(i)
	}
	return term.Caps{
		Width:   80,
		Height:  24,
		Dark:    dark,
		Profile: colorprofile.TrueColor,
		Palette: theme.Probed(slots, fg, bg, dark),
	}
}

// rgbEscape formats a palette colour the way the truecolour formatter does, so
// tests can look for a colour in the output without hand-writing escapes.
func rgbEscape(t *testing.T, hex string) string {
	t.Helper()
	var r, g, b int
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil {
		t.Fatalf("parsing %s: %v", hex, err)
	}
	return fmt.Sprintf("38;2;%d;%d;%d", r, g, b)
}

// TestInlineCodeSitsOnTheTerminalBackground is the regression test for inline
// code that was hard to read on a dark terminal. Code used to be painted on a
// lighter shade of the background, which sounds like it adds definition but does
// the opposite: a colour is chosen against the terminal's background, so moving
// that background costs contrast. The colour alone marks the code.
func TestInlineCodeSitsOnTheTerminalBackground(t *testing.T) {
	caps := probedCaps(true,
		color.RGBA{R: 0xd8, G: 0xd8, B: 0xd8, A: 0xff},
		color.RGBA{R: 0x1c, G: 0x1c, B: 0x1c, A: 0xff})
	out, err := New(caps, Options{MaxWidth: 80}).Render("Some `code` here.\n", 80)
	if err != nil {
		t.Fatal(err)
	}

	for _, background := range []string{"48;2;", "48;5;"} {
		if strings.Contains(out, background) {
			t.Errorf("inline code is painted on a background of its own (%s):\n%q", background, out)
		}
	}
	want := rgbEscape(t, caps.Palette.HexSlot(theme.BrightBlue))
	if !strings.Contains(out, want) {
		t.Errorf("inline code should still be coloured with the accent (%s):\n%q", want, out)
	}
}

// TestCodeTextFollowsTheTerminalForeground covers the other half of the same
// problem: Chroma was told that plain code text is the white slot, which on a
// light theme is white text on a white background.
func TestCodeTextFollowsTheTerminalForeground(t *testing.T) {
	fg := color.RGBA{R: 0x28, G: 0x28, B: 0x28, A: 0xff}
	caps := probedCaps(false, fg, color.RGBA{R: 0xfa, G: 0xfa, B: 0xfa, A: 0xff})
	out, err := New(caps, Options{MaxWidth: 80}).Render("```go\npackage main\n```\n", 80)
	if err != nil {
		t.Fatal(err)
	}

	if want := rgbEscape(t, theme.Hex(fg)); !strings.Contains(out, want) {
		t.Errorf("plain code text should use the terminal's foreground %s, want %s in:\n%q",
			theme.Hex(fg), want, out)
	}
	if unwanted := rgbEscape(t, caps.Palette.HexSlot(theme.White)); strings.Contains(out, unwanted) {
		t.Errorf("plain code text is painted in the white slot (%s), which a light background swallows:\n%q",
			unwanted, out)
	}
}

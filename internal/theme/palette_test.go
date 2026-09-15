package theme

import (
	"fmt"
	"image/color"
	"math"
	"regexp"
	"testing"
)

var hexPattern = regexp.MustCompile(`^#[0-9a-f]{6}$`)

func probedPalette() Palette {
	var slots [NumSlots]color.RGBA
	for i := range NumSlots {
		slots[i] = xtermRGB[i]
	}
	slots[Red] = color.RGBA{R: 0xff, G: 0x00, B: 0x80, A: 0xff}
	return Probed(slots,
		color.RGBA{R: 0xd0, G: 0xd0, B: 0xd0, A: 0xff},
		color.RGBA{R: 0x10, G: 0x10, B: 0x18, A: 0xff},
		true)
}

// TestDefaultRefersToAnsiIndices pins the fallback strategy: without a probed
// palette, colours must be expressed as ANSI slot indices so the terminal paints
// them with the user's own scheme.
func TestDefaultRefersToAnsiIndices(t *testing.T) {
	p := Default(true)

	if p.Tier != TierANSI {
		t.Errorf("Tier = %v, want TierANSI", p.Tier)
	}
	if got := p.Slot(Red); got == nil || *got != "1" {
		t.Errorf("Slot(Red) = %v, want the index \"1\"", got)
	}
	if got := p.Slot(BrightCyan); got == nil || *got != "14" {
		t.Errorf("Slot(BrightCyan) = %v, want the index \"14\"", got)
	}
	// The default foreground is deliberately unpinned: heading text and body
	// text should keep whatever colour the terminal is already using.
	if got := p.Foreground(); got != nil {
		t.Errorf("Foreground() = %v, want nil so the terminal default is used", *got)
	}
	// Chroma cannot express indices, so it always gets real RGB values.
	if got := p.HexSlot(Red); got != "#cd0000" {
		t.Errorf("HexSlot(Red) = %s, want the xterm default #cd0000", got)
	}
}

func TestProbedUsesReadBackValues(t *testing.T) {
	p := probedPalette()

	if p.Tier != TierRGB {
		t.Errorf("Tier = %v, want TierRGB", p.Tier)
	}
	if got := p.Slot(Red); got == nil || *got != "#ff0080" {
		t.Errorf("Slot(Red) = %v, want the probed #ff0080", got)
	}
	if got := p.HexSlot(Red); got != "#ff0080" {
		t.Errorf("HexSlot(Red) = %s, want #ff0080", got)
	}
	if got := p.Foreground(); got == nil || *got != "#d0d0d0" {
		t.Errorf("Foreground() = %v, want the probed #d0d0d0", got)
	}
}

// TestMonoHasNoColour checks that a mono palette never hands out a colour, which
// is what makes --no-color produce genuinely plain output.
func TestMonoHasNoColour(t *testing.T) {
	p := Mono(true)

	if p.Tier != TierMono {
		t.Errorf("Tier = %v, want TierMono", p.Tier)
	}
	for name, got := range map[string]*string{
		"Slot":       p.Slot(Red),
		"Foreground": p.Foreground(),
		"Panel":      p.Panel(),
		"Dim":        p.Dim(),
		"Rule":       p.Rule(),
		"Heading":    p.Heading(),
		"Accent":     p.Accent(),
		"Link":       p.Link(),
		"Shade":      p.Shade(Blue, 0.2),
	} {
		if got != nil {
			t.Errorf("%s() = %q, want nil with colour disabled", name, *got)
		}
	}
}

func TestPanelIsSubtleAndValid(t *testing.T) {
	dark, light := Default(true).Panel(), Default(false).Panel()
	if dark == nil || *dark != "236" {
		t.Errorf("dark panel = %v, want \"236\"", dark)
	}
	if light == nil || *light != "254" {
		t.Errorf("light panel = %v, want \"254\"", light)
	}

	// A probed palette derives its panel from the terminal's own colours.
	p := probedPalette()
	panel := p.Panel()
	if panel == nil || !hexPattern.MatchString(*panel) {
		t.Fatalf("probed panel = %v, want an rgb hex value", panel)
	}
	if *panel == Hex(p.BG) {
		t.Error("the panel colour should differ from the background")
	}
}

func TestShadeClamps(t *testing.T) {
	p := Default(true)
	for _, amount := range []float64{-2, -1, -0.5, 0, 0.5, 1, 2} {
		got := p.Shade(Red, amount)
		if got == nil || !hexPattern.MatchString(*got) {
			t.Errorf("Shade(Red, %v) = %v, want a valid rgb hex value", amount, got)
		}
	}
	if got := *p.Shade(Red, -1); got != "#000000" {
		t.Errorf("Shade(Red, -1) = %s, want black", got)
	}
	if got := *p.Shade(Red, 1); got != "#ffffff" {
		t.Errorf("Shade(Red, 1) = %s, want white", got)
	}
}

func TestLuminance(t *testing.T) {
	cases := []struct {
		in   color.RGBA
		want float64
	}{
		{in: color.RGBA{R: 0x00, G: 0x00, B: 0x00, A: 0xff}, want: 0},
		{in: color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, want: 1},
	}
	for _, tc := range cases {
		if got := Luminance(tc.in); got != tc.want {
			t.Errorf("Luminance(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
	// The threshold used to classify a terminal background.
	if Luminance(color.RGBA{R: 0x1e, G: 0x1e, B: 0x1e, A: 0xff}) >= 0.5 {
		t.Error("a near-black background should classify as dark")
	}
	if Luminance(color.RGBA{R: 0xfa, G: 0xfa, B: 0xfa, A: 0xff}) < 0.5 {
		t.Error("a near-white background should classify as light")
	}
}

func TestHexFormat(t *testing.T) {
	if got := Hex(color.RGBA{R: 0x00, G: 0x0a, B: 0xff, A: 0xff}); got != "#000aff" {
		t.Errorf("Hex() = %s, want #000aff", got)
	}
}

// TestColorHelperHandlesNil keeps the lipgloss bridge honest: a palette that
// hands back nil must turn into "no colour" rather than a zero-value colour.
func TestColorHelperHandlesNil(t *testing.T) {
	if _, ok := Color(nil).(color.Color); !ok {
		t.Error("Color(nil) should still be a colour")
	}
	if got := Color(nil); got != nil {
		if _, isNoColor := got.(interface {
			RGBA() (uint32, uint32, uint32, uint32)
		}); !isNoColor {
			t.Errorf("Color(nil) = %#v, want a no-colour value", got)
		}
	}
}

func TestDefaultSlotBounds(t *testing.T) {
	if got := Hex(DefaultSlot(Red)); got != "#cd0000" {
		t.Errorf("DefaultSlot(Red) = %s, want #cd0000", got)
	}
	for _, i := range []int{-1, NumSlots, 99} {
		if got := Hex(DefaultSlot(i)); got != "#000000" {
			t.Errorf("DefaultSlot(%d) = %s, want black for an out-of-range slot", i, got)
		}
	}
}

// TestCodeTextFollowsTheBackground guards the case that made code unreadable:
// Chroma was given the white slot as its text colour, which on a light theme is
// white text on a white background, and on a dark theme is a guess at what the
// terminal's text looks like rather than the real thing.
func TestCodeTextFollowsTheBackground(t *testing.T) {
	if got, want := Default(true).CodeText(), Hex(DefaultSlot(White)); got != want {
		t.Errorf("dark CodeText() = %s, want the terminal's white %s", got, want)
	}
	if got, want := Default(false).CodeText(), Hex(DefaultSlot(Black)); got != want {
		t.Errorf("light CodeText() = %s, want the terminal's black %s", got, want)
	}
	if got, want := probedPalette().CodeText(), "#d0d0d0"; got != want {
		t.Errorf("probed CodeText() = %s, want the probed foreground %s", got, want)
	}
}

// TestCodeSlotPicksTheHalfThatShows pins the rule that decides which form of a
// colour a syntax token gets.
func TestCodeSlotPicksTheHalfThatShows(t *testing.T) {
	dark, light := Default(true), Default(false)

	if got, want := dark.CodeSlot(BrightGreen, Green), Hex(DefaultSlot(BrightGreen)); got != want {
		t.Errorf("dark CodeSlot(BrightGreen, Green) = %s, want the bright %s", got, want)
	}
	if got, want := light.CodeSlot(BrightGreen, Green), Hex(DefaultSlot(Green)); got != want {
		t.Errorf("light CodeSlot(BrightGreen, Green) = %s, want the normal %s", got, want)
	}
	if got, want := probedPalette().CodeSlot(BrightCyan, Cyan), "#00ffff"; got != want {
		t.Errorf("probed CodeSlot(BrightCyan, Cyan) = %s, want #00ffff", got)
	}
}

func TestCodeBackgroundIsTheTerminalBackground(t *testing.T) {
	if got, want := probedPalette().CodeBackground(), "#101018"; got != want {
		t.Errorf("CodeBackground() = %s, want the terminal's own %s", got, want)
	}
}

// lightProbedPalette is the same probe as probedPalette, read on a terminal with
// a light background.
func lightProbedPalette() Palette {
	var slots [NumSlots]color.RGBA
	for i := range NumSlots {
		slots[i] = xtermRGB[i]
	}
	return Probed(slots,
		color.RGBA{R: 0x28, G: 0x28, B: 0x28, A: 0xff},
		color.RGBA{R: 0xfa, G: 0xfa, B: 0xfa, A: 0xff},
		false)
}

// TestCodeTextIsReadableOnItsBackground states the guarantee in the terms that
// matter, rather than in hex values: however the palette was obtained, text in a
// code block has to be readable against the background it lands on.
func TestCodeTextIsReadableOnItsBackground(t *testing.T) {
	for _, c := range []struct {
		name string
		p    Palette
	}{
		{"dark default", Default(true)},
		{"light default", Default(false)},
		{"dark probed", probedPalette()},
		{"light probed", lightProbedPalette()},
	} {
		t.Run(c.name, func(t *testing.T) {
			text := parseHex(t, c.p.CodeText())
			if got := contrastRatio(text, c.p.BG); got < 4.5 {
				t.Errorf("code text %s on background %s is %.1f:1, want at least 4.5:1",
					c.p.CodeText(), Hex(c.p.BG), got)
			}
		})
	}
}

// TestAccentIsVisibleOnItsBackground covers the colour inline code is drawn in.
// The accent is the terminal's own blue or magenta, so md cannot guarantee it a
// ratio the way it does for code text - a palette is free to define its blue as
// it likes - but it must at least stay visible.
func TestAccentIsVisibleOnItsBackground(t *testing.T) {
	for _, p := range []Palette{Default(true), Default(false), probedPalette(), lightProbedPalette()} {
		accent := *p.Accent()
		if !hexPattern.MatchString(accent) {
			continue // an ANSI index: the terminal paints it, so md cannot measure it
		}
		if got := contrastRatio(parseHex(t, accent), p.BG); got < 3 {
			t.Errorf("accent %s on background %s is %.1f:1, want at least 3:1",
				accent, Hex(p.BG), got)
		}
	}
}

// parseHex turns a palette colour back into RGB for contrast arithmetic.
func parseHex(t *testing.T, s string) color.RGBA {
	t.Helper()
	var c color.RGBA
	if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &c.R, &c.G, &c.B); err != nil {
		t.Fatalf("parsing %s: %v", s, err)
	}
	c.A = 0xff
	return c
}

// contrastRatio is the WCAG contrast ratio between two colours: 1:1 for two
// identical colours and 21:1 for black against white.
func contrastRatio(a, b color.RGBA) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func relativeLuminance(c color.RGBA) float64 {
	channel := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}

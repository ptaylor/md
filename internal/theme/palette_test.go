package theme

import (
	"image/color"
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

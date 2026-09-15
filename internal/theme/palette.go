// Package theme supplies the colour palette md renders with.
//
// The palette is deliberately anchored to the terminal's own colours. When the
// terminal answers colour queries (OSC 4/10/11) the exact RGB values the user
// has configured are used. When it does not, md refers to the 16 ANSI slots by
// index instead, so the terminal paints them with whatever scheme is active.
// Either way the rendered document matches the user's terminal theme.
package theme

import (
	"fmt"
	"image/color"
	"strconv"

	"charm.land/lipgloss/v2"
)

// Tier describes how much the terminal told md about its own colours.
type Tier int

const (
	// TierMono renders structure without colour.
	TierMono Tier = iota
	// TierANSI references colours by ANSI slot index (0-15), letting the
	// terminal paint them with the user's active colour scheme.
	TierANSI
	// TierRGB uses exact RGB values read back from the terminal.
	TierRGB
)

// The 16 ANSI colour slots.
const (
	Black = iota
	Red
	Green
	Yellow
	Blue
	Magenta
	Cyan
	White
	BrightBlack
	BrightRed
	BrightGreen
	BrightYellow
	BrightBlue
	BrightMagenta
	BrightCyan
	BrightWhite
	// NumSlots is the number of ANSI colour slots.
	NumSlots = 16
)

// xtermRGB is the default xterm palette. It is used when the terminal does not
// answer colour queries, and it always feeds Chroma, which cannot express ANSI
// colour indices.
var xtermRGB = [NumSlots]color.RGBA{
	{R: 0x00, G: 0x00, B: 0x00, A: 0xff}, // black
	{R: 0xcd, G: 0x00, B: 0x00, A: 0xff}, // red
	{R: 0x00, G: 0xcd, B: 0x00, A: 0xff}, // green
	{R: 0xcd, G: 0xcd, B: 0x00, A: 0xff}, // yellow
	{R: 0x00, G: 0x00, B: 0xee, A: 0xff}, // blue
	{R: 0xcd, G: 0x00, B: 0xcd, A: 0xff}, // magenta
	{R: 0x00, G: 0xcd, B: 0xcd, A: 0xff}, // cyan
	{R: 0xe5, G: 0xe5, B: 0xe5, A: 0xff}, // white
	{R: 0x7f, G: 0x7f, B: 0x7f, A: 0xff}, // bright black
	{R: 0xff, G: 0x00, B: 0x00, A: 0xff}, // bright red
	{R: 0x00, G: 0xff, B: 0x00, A: 0xff}, // bright green
	{R: 0xff, G: 0xff, B: 0x00, A: 0xff}, // bright yellow
	{R: 0x5c, G: 0x5c, B: 0xff, A: 0xff}, // bright blue
	{R: 0xff, G: 0x00, B: 0xff, A: 0xff}, // bright magenta
	{R: 0x00, G: 0xff, B: 0xff, A: 0xff}, // bright cyan
	{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, // bright white
}

// Palette is the set of colours md renders with.
type Palette struct {
	// Tier records how the colours were obtained.
	Tier Tier
	// Dark reports whether the terminal background is dark.
	Dark bool
	// FG and BG are the terminal's default foreground and background.
	FG, BG color.RGBA
	// rgb always holds 16 slots: probed values for TierRGB, xterm defaults
	// otherwise.
	rgb [NumSlots]color.RGBA
}

// Default returns the palette used when the terminal cannot be queried. Colours
// are referenced by ANSI index so the terminal supplies the actual values.
func Default(dark bool) Palette {
	p := Palette{Tier: TierANSI, Dark: dark, rgb: xtermRGB}
	if dark {
		p.FG = color.RGBA{R: 0xd8, G: 0xd8, B: 0xd8, A: 0xff}
		p.BG = color.RGBA{R: 0x14, G: 0x14, B: 0x14, A: 0xff}
	} else {
		p.FG = color.RGBA{R: 0x28, G: 0x28, B: 0x28, A: 0xff}
		p.BG = color.RGBA{R: 0xfa, G: 0xfa, B: 0xfa, A: 0xff}
	}
	return p
}

// Probed builds a palette from values read back from the terminal.
func Probed(slots [NumSlots]color.RGBA, fg, bg color.RGBA, dark bool) Palette {
	return Palette{Tier: TierRGB, Dark: dark, FG: fg, BG: bg, rgb: slots}
}

// Mono returns a palette that carries no colour at all.
func Mono(dark bool) Palette {
	return Palette{Tier: TierMono, Dark: dark, rgb: xtermRGB}
}

// DefaultSlot returns the xterm default colour for an ANSI slot. It is used to
// fill in slots a terminal did not report.
func DefaultSlot(i int) color.RGBA {
	if i < 0 || i >= NumSlots {
		return xtermRGB[Black]
	}
	return xtermRGB[i]
}

// Luminance reports how dark an RGB colour is, in the range 0 (black) to 1
// (white).
func Luminance(c color.RGBA) float64 {
	return (0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)) / 255
}

// Slot returns the colour of an ANSI slot in the form glamour expects: an ANSI
// index when the terminal paints the colours, an RGB hex value when the exact
// colour was read back, and nil when colour is disabled.
func (p Palette) Slot(i int) *string {
	switch p.Tier {
	case TierMono:
		return nil
	case TierRGB:
		return strPtr(Hex(p.rgb[i]))
	default:
		return strPtr(strconv.Itoa(i))
	}
}

// Shade returns slot i lightened (amount > 0) or darkened (amount < 0) by the
// given fraction.
func (p Palette) Shade(i int, amount float64) *string {
	if p.Tier == TierMono {
		return nil
	}
	return strPtr(Hex(shade(p.rgb[i], amount)))
}

// HexSlot always returns an RGB hex value, which is the only form Chroma
// understands. Probed values are used when available, the xterm defaults
// otherwise.
func (p Palette) HexSlot(i int) string { return Hex(p.rgb[i]) }

// Foreground returns the terminal's default foreground colour.
func (p Palette) Foreground() *string {
	if p.Tier == TierMono {
		return nil
	}
	if p.Tier == TierRGB {
		return strPtr(Hex(p.FG))
	}
	return nil // the terminal's default foreground needs no escape sequence
}

// Dim returns a muted colour for secondary text such as metadata and code
// fences.
func (p Palette) Dim() *string { return p.Slot(BrightBlack) }

// Rule returns the colour used for horizontal rules and box frames.
func (p Palette) Rule() *string { return p.Slot(BrightBlack) }

// Panel returns a subtly raised background for panels such as the front matter
// block, the status bar and the mermaid source box.
func (p Palette) Panel() *string {
	switch p.Tier {
	case TierMono:
		return nil
	case TierRGB:
		return strPtr(Hex(blend(p.BG, p.FG, 0.09)))
	default:
		if p.Dark {
			return strPtr("236")
		}
		return strPtr("254")
	}
}

// Heading returns the colour used for headings, derived so it stays legible on
// the terminal's own background.
func (p Palette) Heading() *string {
	if p.Dark {
		return p.Slot(BrightCyan)
	}
	return p.Slot(Blue)
}

// Accent returns the colour used for rules, bullets and inline emphasis.
func (p Palette) Accent() *string {
	if p.Dark {
		return p.Slot(BrightBlue)
	}
	return p.Slot(Magenta)
}

// Link returns the colour used for link text and URLs.
func (p Palette) Link() *string {
	if p.Dark {
		return p.Slot(BrightBlue)
	}
	return p.Slot(Blue)
}

// Color converts a palette colour into a lipgloss colour, treating nil (colour
// disabled) as "leave whatever the terminal is using".
func Color(s *string) color.Color {
	if s == nil {
		return lipgloss.NoColor{}
	}
	return lipgloss.Color(*s)
}

// Hex formats an RGB colour the way glamour, lipgloss and Chroma all accept it.
func Hex(c color.RGBA) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// blend mixes a towards b by t (0..1).
func blend(a, b color.RGBA, t float64) color.RGBA {
	mix := func(x, y uint8) uint8 {
		return uint8(float64(x) + (float64(y)-float64(x))*t) //nolint:gosec
	}
	return color.RGBA{R: mix(a.R, b.R), G: mix(a.G, b.G), B: mix(a.B, b.B), A: 0xff}
}

// shade lightens (amount > 0) or darkens (amount < 0) a colour. The factor is
// clamped so that an out-of-range value cannot wrap around.
func shade(c color.RGBA, amount float64) color.RGBA {
	target := color.RGBA{A: 0xff} // black
	if amount > 0 {
		target = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	}
	t := min(max(amount, -1), 1)
	if t < 0 {
		t = -t
	}
	return blend(c, target, t)
}

func strPtr(s string) *string { return &s }

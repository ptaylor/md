package render

import (
	"strings"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/pftylr/md/internal/theme"
)

// ruleWidth is how many cells a horizontal rule may occupy: the content width
// less the indent the rule is written at, so that it never wraps onto a second
// line.
func ruleWidth(content int) int {
	return max(content-docMargin-levelIndent, 8)
}

// fitsPanel reports whether a drawing fits the space its panel leaves for
// content. The panel's own frame and gutter take four cells.
func fitsPanel(lines []string, avail int) bool {
	for _, line := range lines {
		if xansi.StringWidth(line) > avail-4 {
			return false
		}
	}
	return true
}

// ruleWidth is the rule length for the render in progress.
func (r *Renderer) ruleWidth() int { return ruleWidth(r.width) }

// ruleGlyph returns the character used for horizontal rules and box edges.
func (r *Renderer) ruleGlyph() string {
	if r.opts.Ascii {
		return "-"
	}
	return "─"
}

// mono reports whether colour is disabled entirely.
func (r *Renderer) mono() bool { return r.caps.Palette.Tier == theme.TierMono }

// paint applies a style, or returns the text untouched when colour is off.
// Every piece of md's own chrome goes through here, so that --no-color and
// NO_COLOR produce genuinely plain output rather than colourless text wrapped in
// escape sequences.
func (r *Renderer) paint(s lipgloss.Style, text string) string {
	if r.mono() {
		return text
	}
	return s.Render(text)
}

// frame returns the border glyphs for the current rendering mode.
func (r *Renderer) frame() (tl, tr, bl, br, h, v string) {
	if r.opts.Ascii {
		return "+", "+", "+", "+", "-", "|"
	}
	return "╭", "╮", "╰", "╯", "─", "│"
}

// panel draws a labelled box around already-styled lines, indented to sit where
// the block it replaces belongs.
//
// Every row is padded to exactly the same width, which is what makes it read as
// a box rather than a set of rules. Content that does not fit is wrapped with a
// continuation marker instead of being truncated, so no code is ever lost.
func (r *Renderer) panel(label string, lines []string, indent, avail int) []string {
	tl, tr, bl, br, h, v := r.frame()
	p := r.caps.Palette
	border := lipgloss.NewStyle().Foreground(theme.Color(p.Rule()))
	labelStyle := lipgloss.NewStyle().Foreground(theme.Color(p.Accent())).Bold(true)
	pad := strings.Repeat(" ", indent)

	if avail < minPanelWidth {
		// Not enough room for a box at all: a gutter still frames the block
		// without truncating anything.
		out := make([]string, 0, len(lines)+1)
		if label != "" {
			out = append(out, pad+r.paint(border, v+" ")+r.paint(labelStyle, label))
		}
		for _, l := range lines {
			out = append(out, pad+r.paint(border, v+" ")+l)
		}
		return out
	}

	natural := 0
	for _, l := range lines {
		natural = max(natural, xansi.StringWidth(l))
	}
	content := minPanelWidth - 4
	if natural > content {
		content = natural
	}
	if maxContent := avail - 4; content > maxContent {
		content = maxContent
	}

	body := make([]string, 0, len(lines))
	for _, l := range lines {
		body = append(body, r.wrapCells(l, content)...)
	}

	out := make([]string, 0, len(body)+2)

	// Top border: "╭─ label ────╮", with the rule cells kept in step with the
	// content width.
	fill := content + 1
	head := r.paint(border, tl+h)
	if label != "" {
		if width := xansi.StringWidth(label); width+2 <= fill {
			head += r.paint(labelStyle, " "+label+" ")
			fill -= width + 2
		}
	}
	head += r.paint(border, strings.Repeat(h, fill)+tr)
	out = append(out, pad+head)

	for _, l := range body {
		gap := content - xansi.StringWidth(l)
		if gap < 0 {
			gap = 0
		}
		out = append(out, pad+r.paint(border, v+" ")+l+strings.Repeat(" ", gap)+r.paint(border, " "+v))
	}

	out = append(out, pad+r.paint(border, bl+strings.Repeat(h, content+2)+br))
	return out
}

// wrapCells splits a styled line into chunks of at most width cells.
// Continuation chunks are prefixed with a marker so wrapped content stays
// legible.
func (r *Renderer) wrapCells(line string, width int) []string {
	if xansi.StringWidth(line) <= width {
		return []string{line}
	}
	out := []string{xansi.TruncateWc(line, width, "")}
	rest := xansi.TruncateLeftWc(line, width, "")
	marker, span := r.continuationMark()
	tail := max(width-span, 1)
	for guard := 0; rest != "" && guard < 512; guard++ {
		out = append(out, marker+xansi.TruncateWc(rest, tail, ""))
		next := xansi.TruncateLeftWc(rest, tail, "")
		if next == rest {
			break // no progress: stop rather than loop forever
		}
		rest = next
	}
	return out
}

// continuationMark returns the styled glyph that marks a wrapped line, and how
// many cells it occupies.
func (r *Renderer) continuationMark() (string, int) {
	glyph := "↳ "
	if r.opts.Ascii {
		glyph = "> "
	}
	style := lipgloss.NewStyle().Foreground(theme.Color(r.caps.Palette.Rule()))
	return r.paint(style, glyph), xansi.StringWidth(glyph)
}

// splitLines splits on newlines and normalises line endings.
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// expandTabs converts tabs to spaces. Code is never re-wrapped blindly, so a
// literal tab would push a panel's right edge out of alignment.
func expandTabs(s string) string {
	return strings.ReplaceAll(s, "\t", "    ")
}

// dropTrailingEmpty removes a single trailing empty line, which is what a code
// block's final newline turns into.
func dropTrailingEmpty(lines []string) []string {
	if n := len(lines); n > 0 && strings.TrimSpace(xansi.Strip(lines[n-1])) == "" {
		return lines[:n-1]
	}
	return lines
}

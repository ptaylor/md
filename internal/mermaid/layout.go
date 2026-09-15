package mermaid

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
)

// A cell is roughly twice as tall as it is wide, so a diagram unit covers half
// as much height per row as width per column. Scale is therefore the number of
// diagram units one column covers, and rows cover twice that: keeping to this
// ratio is what stops circles looking like eggs.
const (
	// minScale is the widest the drawing is allowed to get, in diagram units
	// per column.
	minScale = 0.75
	// maxScale is the tightest, and stops a diagram with one small box from
	// being scaled up to fill the terminal.
	maxScale = 64.0
	// pad is the breathing space left inside a box, in cells.
	pad = 1
)

// layout is a diagram's scale and the size of the grid it needs.
type layout struct {
	// scale is diagram units per column.
	scale float64
	// cols and rows are the grid's dimensions in cells.
	cols, rows int
}

// layoutFor chooses how large to draw a diagram.
//
// The scale is the largest at which every label still fits inside the box mermaid
// drew for it, which makes the drawing as compact as the text allows. Labels are
// wrapped rather than truncated, so a long label makes its box taller rather than
// losing words; if even the most compact drawing is wider than the space
// available, the diagram is refused and the caller shows the source instead.
func layoutFor(d Diagram, avail int) (layout, bool) {
	if len(d.Nodes) == 0 || d.Width <= 0 || d.Height <= 0 {
		return layout{}, false
	}
	// Prefer a scale where no word has to be broken to fit: a box with "cache"
	// split over two lines reads as a mistake, where a slightly larger box does
	// not. Only when no such scale exists does breaking a word become the
	// lesser evil.
	lo, ok := largestFitting(d, fitsWholeWords)
	if !ok {
		if lo, ok = largestFitting(d, fitsAt); !ok {
			return layout{}, false
		}
	}
	l := layout{scale: lo, cols: cells(d.Width, lo) + 1, rows: rows(d.Height, lo) + 1}
	if l.cols > avail {
		return layout{}, false
	}
	return l, true
}

// largestFitting bisects for the largest scale at which every label fits, given
// a test. Larger scales mean smaller boxes, so fitting gets harder as the scale
// rises, which is what makes bisection work.
func largestFitting(d Diagram, fits func(Diagram, float64) bool) (float64, bool) {
	lo, hi := minScale, maxScale
	if !fits(d, lo) {
		return 0, false
	}
	for range 24 {
		mid := (lo + hi) / 2
		if fits(d, mid) {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo, true
}

// cells is how many columns a distance covers at a scale.
func cells(units, scale float64) int { return int(units/scale + 0.5) }

// rows is how many rows a distance covers at a scale.
func rows(units, scale float64) int { return int(units/(2*scale) + 0.5) }

// fitsAt reports whether every label still fits its box at a scale. The room
// measured here has to be the room the drawing code uses, or a label will be
// wrapped to fewer cells than it is given and lose words.
func fitsAt(d Diagram, scale float64) bool {
	for _, n := range d.Nodes {
		cols := innerWidth(cells(n.Box.W, scale))
		rows := rows(n.Box.H, scale) - 2
		if cols < 1 || rows < 1 {
			return false
		}
		if len(wrapCells(n.Label, cols)) > rows {
			return false
		}
	}
	return true
}

// fitsWholeWords is fitsAt with the added condition that no word has to be
// broken to fit the box.
func fitsWholeWords(d Diagram, scale float64) bool {
	if !fitsAt(d, scale) {
		return false
	}
	for _, n := range d.Nodes {
		cols := innerWidth(cells(n.Box.W, scale))
		for _, line := range n.Label {
			for _, word := range strings.Fields(line) {
				if xansi.StringWidth(word) > cols {
					return false
				}
			}
		}
	}
	return true
}

// innerWidth is the room inside a frame: the two border cells and the breathing
// space on either side are not available to a label.
func innerWidth(frame int) int { return frame - 2 - 2*pad }

// wrapCells wraps label lines to a width in cells, breaking a word that is longer
// than the box rather than letting it overflow the frame.
func wrapCells(lines []string, cols int) []string {
	if cols < 1 {
		return nil
	}
	var out []string
	for _, line := range lines {
		out = append(out, wrapLine(line, cols)...)
	}
	return out
}

func wrapLine(line string, cols int) []string {
	words := strings.Fields(line)
	if len(words) == 0 {
		return []string{""}
	}
	var out []string
	cur := ""
	for _, word := range words {
		for xansi.StringWidth(word) > cols {
			// A single word too long for the box: take as much as fits.
			head, tail := splitCells(word, cols)
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			out = append(out, head)
			word = tail
		}
		switch {
		case cur == "":
			cur = word
		case xansi.StringWidth(cur)+1+xansi.StringWidth(word) <= cols:
			cur += " " + word
		default:
			out = append(out, cur)
			cur = word
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// splitCells splits a string after n cells.
func splitCells(s string, n int) (head, tail string) {
	width := 0
	for i, r := range s {
		w := xansi.StringWidth(string(r))
		if width+w > n {
			return s[:i], s[i:]
		}
		width += w
	}
	return s, ""
}

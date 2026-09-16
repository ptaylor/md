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
// wrapped to fewer cells than it is given and lose words, which is why both go
// through nodeLabel.
func fitsAt(d Diagram, scale float64) bool {
	for _, n := range d.Nodes {
		if _, _, _, ok := nodeLabel(n, cells(n.Box.W, scale), rows(n.Box.H, scale)); !ok {
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
		_, room, _, ok := nodeLabel(n, cells(n.Box.W, scale), rows(n.Box.H, scale))
		if !ok {
			return false
		}
		for _, line := range n.Label {
			for _, word := range strings.Fields(line) {
				if xansi.StringWidth(word) > room {
					return false
				}
			}
		}
	}
	return true
}

// nodeLabel wraps a node's label to the room its frame gives it, and reports the
// row of the frame the first line sits on, counted from the frame's top.
//
// Layout and drawing both come through here, because the width a label is wrapped
// to is a promise to the drawing: a shape that is narrow away from its middle has
// no single width, so a caller working it out for itself would lose words.
func nodeLabel(n Node, w, h int) (lines []string, room, top int, ok bool) {
	if n.Shape == Diamond {
		return diamondLabel(n.Label, w, h)
	}
	room = innerWidth(w)
	if room < 1 || h-2 < 1 {
		return nil, 0, 0, false
	}
	lines = wrapCells(n.Label, room)
	if len(lines) > h-2 {
		return nil, 0, 0, false
	}
	// A frame has a border top and bottom, and the label sits between them.
	return lines, room, (h - len(lines)) / 2, true
}

// diamondFrame trims a frame to the odd size a pointed box is drawn at, so that
// its apex sits exactly between its corners instead of half a cell to one side.
func diamondFrame(w, h int) (int, int) {
	if w%2 == 0 {
		w--
	}
	if h%2 == 0 {
		h--
	}
	return w, h
}

// pointed is one row of a pointed box's outline: the columns its two sides are
// drawn at, and how many cells each side crosses besides its own glyph.
type pointed struct {
	left, right, tread int
}

// room is the run of cells between the sides on this row, which is what a label
// sitting on the row has to work with.
func (r pointed) room() int { return max(r.right-r.left-2*r.tread-1, 0) }

// diamondRows gives the outline of a pointed box, row by row.
//
// A pointed box is twice as wide as it is tall in cells: a cell is about twice as
// tall as it is wide, and mermaid draws a diamond as wide as it is high in its
// own units. Its sides therefore run two columns sideways for every row they
// drop, which one glyph per row cannot show - the steps would not join up. So
// each row carries the run of cells its side crosses on its way down: a slash
// where the side lands, and dashes for the row it crossed to get there.
func diamondRows(w, h int) []pointed {
	w, h = diamondFrame(w, h)
	if w < 3 || h < 3 {
		return nil
	}
	midX, midY := (w-1)/2, (h-1)/2
	// off is how far a row's sides are from the frame's centre: the corners are
	// the full half width away, the apexes none of it.
	off := func(d int) int { return ((midY-d)*midX + midY/2) / midY }
	out := make([]pointed, h)
	for y := range out {
		d := abs(y - midY)
		reach := off(d)
		// A row's tread is the ground covered coming down from the row nearer
		// the apex, less the glyph that stands for the landing itself.
		tread := 0
		if d < midY {
			tread = max(reach-off(d+1)-1, 0)
		}
		out[y] = pointed{left: midX - reach, right: midX + reach, tread: tread}
	}
	return out
}

// diamondLabel lays a label out inside a pointed box.
//
// The box is only wide near its middle, so how much room the label has depends on
// how many rows it needs, and how many rows it needs depends on how much room it
// has. The two are untangled by trying the shortest block of rows that could hold
// it: fewest lines wins, which keeps a short label on the single row where the
// box is widest.
func diamondLabel(label []string, w, h int) (lines []string, room, top int, ok bool) {
	rows := diamondRows(w, h)
	if rows == nil {
		return nil, 0, 0, false
	}
	h = len(rows)
	mid := (h - 1) / 2
	for k := 1; k <= h-2; k++ {
		first := mid - (k-1)/2
		if first < 1 || first+k > h-1 {
			break // the block would have to cover an apex, which has no room
		}
		room = roomFor(rows, first, k)
		if room < 1 {
			continue
		}
		lines = wrapCells(label, room)
		switch {
		case len(lines) > k:
			continue
		case len(lines) < k:
			// The wrap came out shorter than the block it was tried in, so it
			// has room to spare: lay it out again at the size it turned out to
			// be, which is nearer the middle and so wider.
			k = len(lines)
			first = mid - (k-1)/2
			room = roomFor(rows, first, k)
			lines = wrapCells(label, room)
		}
		return lines, room, first, true
	}
	return nil, 0, 0, false
}

// roomFor is the narrowest run across a block of rows, which is all a label laid
// over them can count on.
func roomFor(rows []pointed, first, k int) int {
	room := rows[first].room()
	for y := first + 1; y < first+k; y++ {
		room = min(room, rows[y].room())
	}
	return room
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

package mermaid

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
)

// Kind says what a cell holds, so the caller can colour it from its own palette.
// md draws diagrams in the terminal's colours, never mermaid's.
type Kind int

const (
	// KindNone is an unused cell.
	KindNone Kind = iota
	// KindEdge is a line between nodes.
	KindEdge
	// KindBorder is a node's outline.
	KindBorder
	// KindText is a node's label.
	KindText
	// KindDim is a secondary label, such as the one on an edge.
	KindDim
)

// Options are the choices a drawing needs from the terminal.
type Options struct {
	// Width is the number of columns available.
	Width int
	// Ascii draws with ASCII characters only.
	Ascii bool
	// Style colours a run of cells. Runs are passed whole so the caller can
	// emit one escape sequence per run rather than one per cell. A nil Style
	// leaves the drawing plain.
	Style func(kind Kind, text string) string
}

// glyphs are the characters a drawing is made of.
type glyphs struct {
	h, v                    rune
	teeUp, teeDown, teeLeft rune
	teeRight, cross         rune
	cornerTL, cornerTR      rune
	cornerBL, cornerBR      rune
	roundTL, roundTR        rune
	roundBL, roundBR        rune
	forward, back           rune
	arrowUp, arrowDown      rune
	arrowLeft, arrowRight   rune
}

func unicodeGlyphs() glyphs {
	return glyphs{
		h: '─', v: '│',
		teeUp: '┴', teeDown: '┬', teeLeft: '┤', teeRight: '├', cross: '┼',
		cornerTL: '┌', cornerTR: '┐', cornerBL: '└', cornerBR: '┘',
		roundTL: '╭', roundTR: '╮', roundBL: '╰', roundBR: '╯',
		forward: '/', back: '\\',
		arrowUp: '▲', arrowDown: '▼', arrowLeft: '◀', arrowRight: '▶',
	}
}

func asciiGlyphs() glyphs {
	return glyphs{
		h: '-', v: '|',
		teeUp: '+', teeDown: '+', teeLeft: '+', teeRight: '+', cross: '+',
		cornerTL: '+', cornerTR: '+', cornerBL: '+', cornerBR: '+',
		roundTL: '+', roundTR: '+', roundBL: '+', roundBR: '+',
		forward: '/', back: '\\',
		arrowUp: '^', arrowDown: 'v', arrowLeft: '<', arrowRight: '>',
	}
}

// Render draws a diagram as lines of text, or reports that it cannot be drawn in
// the space available, in which case the caller shows the source instead.
func Render(d Diagram, o Options) ([]string, bool) {
	l, ok := layoutFor(d, o.Width)
	if !ok {
		return nil, false
	}
	gl := unicodeGlyphs()
	if o.Ascii {
		gl = asciiGlyphs()
	}
	g := newGrid(l.cols, l.rows)

	// Edges go down first so that the boxes sit on top of the lines that reach
	// them, the way mermaid draws them.
	for _, e := range d.Edges {
		drawEdge(g, e, d.Nodes, l, gl)
	}
	for _, n := range d.Nodes {
		drawNode(g, n, l, gl)
	}
	for _, t := range d.Texts {
		drawLabel(g, t, l)
	}
	return g.lines(o.Style), true
}

// grid is a character canvas.
type grid struct {
	w, h int
	ch   []rune
	kind []Kind
	// dirs remembers which way a line leaves each cell, so that crossing and
	// joining lines resolve to the right glyph instead of overdrawing each other.
	dirs []uint8
}

// Direction bits for a cell's line connections.
const (
	dirUp uint8 = 1 << iota
	dirRight
	dirDown
	dirLeft
)

func newGrid(w, h int) *grid {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return &grid{
		w: w, h: h,
		ch:   make([]rune, w*h),
		kind: make([]Kind, w*h),
		dirs: make([]uint8, w*h),
	}
}

func (g *grid) inside(x, y int) bool { return x >= 0 && y >= 0 && x < g.w && y < g.h }

func (g *grid) set(x, y int, ch rune, k Kind) {
	if !g.inside(x, y) {
		return
	}
	i := y*g.w + x
	g.ch[i] = ch
	if k != KindNone {
		g.kind[i] = k
	}
}

// clear empties a cell, including any line that ran through it.
func (g *grid) clear(x, y int) {
	if !g.inside(x, y) {
		return
	}
	i := y*g.w + x
	g.ch[i] = 0
	g.kind[i] = KindNone
	g.dirs[i] = 0
}

func (g *grid) text(x, y int, s string, k Kind) {
	for _, r := range s {
		w := xansi.StringWidth(string(r))
		if w < 1 {
			w = 1
		}
		g.set(x, y, r, k)
		// A wide glyph occupies the cell after it, which must be left blank so
		// the line still measures correctly.
		for i := 1; i < w; i++ {
			g.set(x+i, y, 0, KindNone)
		}
		x += w
	}
}

// segment draws a straight line between two cells, recording the direction so
// that junctions come out as ┼, ├ and friends.
func (g *grid) segment(x0, y0, x1, y1 int, k Kind, gl glyphs) {
	dx, dy := sign(x1-x0), sign(y1-y0)
	x, y := x0, y0
	for i := 0; i <= max(abs(x1-x0), abs(y1-y0)); i++ {
		nx, ny := x+dx, y+dy
		if i == max(abs(x1-x0), abs(y1-y0)) {
			nx, ny = x, y
		}
		g.connect(x, y, nx, ny, k)
		x, y = nx, ny
	}
	g.resolve(k, gl)
}

// connect records that a line passes from one cell to the next.
func (g *grid) connect(x, y, nx, ny int, k Kind) {
	var from, to uint8
	switch {
	case ny < y:
		from, to = dirUp, dirDown
	case ny > y:
		from, to = dirDown, dirUp
	case nx < x:
		from, to = dirLeft, dirRight
	case nx > x:
		from, to = dirRight, dirLeft
	default:
		return
	}
	if diag := nx != x && ny != y; diag {
		// A diagonal step also travels sideways, and has no junction glyph
		// worth the name: the slant is kept as a character and used only when
		// nothing else claims the cell.
		from, to = 0, 0
		if g.inside(x, y) {
			i := y*g.w + x
			if g.ch[i] == 0 {
				g.ch[i] = diagRune(nx > x, ny > y)
			}
		}
		if g.inside(nx, ny) {
			j := ny*g.w + nx
			if g.ch[j] == 0 {
				g.ch[j] = diagRune(nx < x, ny > y)
			}
		}
	}
	if g.inside(x, y) {
		i := y*g.w + x
		g.dirs[i] |= from
		if g.kind[i] == KindNone {
			g.kind[i] = k
		}
	}
	if g.inside(nx, ny) {
		j := ny*g.w + nx
		g.dirs[j] |= to
		if g.kind[j] == KindNone {
			g.kind[j] = k
		}
	}
}

// diagRune returns the slant used for a step that travels right and down, left
// and down, and so on.
func diagRune(right, down bool) rune {
	if right == down {
		return '\\'
	}
	return '/'
}

// resolve fills in the glyph of every cell that carries line directions.
func (g *grid) resolve(k Kind, gl glyphs) {
	for i := range g.dirs {
		d := g.dirs[i]
		if d == 0 {
			continue
		}
		rounded := k == KindEdge
		g.ch[i] = junction(d, rounded, gl)
	}
}

// junction returns the glyph for a set of line directions.
func junction(d uint8, rounded bool, gl glyphs) rune {
	switch d {
	case dirUp, dirDown:
		return gl.v
	case dirLeft, dirRight:
		return gl.h
	case dirUp | dirDown:
		return gl.v
	case dirLeft | dirRight:
		return gl.h
	case dirUp | dirRight:
		return pick(rounded, gl.roundBL, gl.cornerBL)
	case dirUp | dirLeft:
		return pick(rounded, gl.roundBR, gl.cornerBR)
	case dirDown | dirRight:
		return pick(rounded, gl.roundTL, gl.cornerTL)
	case dirDown | dirLeft:
		return pick(rounded, gl.roundTR, gl.cornerTR)
	case dirUp | dirDown | dirRight:
		return gl.teeLeft
	case dirUp | dirDown | dirLeft:
		return gl.teeRight
	case dirLeft | dirRight | dirDown:
		return gl.teeDown
	case dirLeft | dirRight | dirUp:
		return gl.teeUp
	default:
		return gl.cross
	}
}

func pick(cond bool, a, b rune) rune {
	if cond {
		return a
	}
	return b
}

// drawBox draws a rectangular outline, clearing the inside so that a line routed
// behind the box does not show through it.
func drawBox(g *grid, x0, y0, x1, y1 int, rounded bool, k Kind, gl glyphs) {
	if x1-x0 < 1 || y1-y0 < 1 {
		return
	}
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			g.clear(x, y)
		}
	}
	tl, tr := pick(rounded, gl.roundTL, gl.cornerTL), pick(rounded, gl.roundTR, gl.cornerTR)
	bl, br := pick(rounded, gl.roundBL, gl.cornerBL), pick(rounded, gl.roundBR, gl.cornerBR)
	g.set(x0, y0, tl, k)
	g.set(x1, y0, tr, k)
	g.set(x0, y1, bl, k)
	g.set(x1, y1, br, k)
	for x := x0 + 1; x < x1; x++ {
		g.set(x, y0, gl.h, k)
		g.set(x, y1, gl.h, k)
	}
	for y := y0 + 1; y < y1; y++ {
		g.set(x0, y, gl.v, k)
		g.set(x1, y, gl.v, k)
	}
}

// drawNode draws a node's outline and its label.
func drawNode(g *grid, n Node, l layout, gl glyphs) {
	// The frame is measured from the box's size, the same way layoutFor measured
	// it when it decided how much room the label needed. Rounding the two
	// corners instead would lose up to a cell and clip a label.
	x0, y0 := toCell(n.Box.X, n.Box.Y, l)
	w, h := cells(n.Box.W, l.scale), rows(n.Box.H, l.scale)
	if w < 3 || h < 3 {
		return
	}
	var sides []pointed
	if n.Shape == Diamond {
		sides = diamondRows(w, h)
		drawDiamond(g, x0, y0, sides, KindBorder, gl)
	} else {
		drawBox(g, x0, y0, x0+w-1, y0+h-1, n.Shape == Round, KindBorder, gl)
	}
	// The label goes in the room nodeLabel measured, which is the room the fit
	// test agreed to when it chose this scale.
	lines, room, top, ok := nodeLabel(n, w, h)
	if !ok {
		return
	}
	for i, text := range lines {
		width := xansi.StringWidth(text)
		left := x0 + 1 + pad + (room-width)/2
		if sides != nil {
			// A pointed box has different room on every row, so a line is
			// centred on the run its own row leaves rather than on the frame.
			row := sides[top+i]
			left = x0 + row.left + row.tread + 1 + (row.room()-width)/2
		}
		g.text(left, y0+top+i, text, KindText)
	}
}

// drawDiamond draws a pointed outline from the rows diamondRows measured.
func drawDiamond(g *grid, x0, y0 int, sides []pointed, k Kind, gl glyphs) {
	if sides == nil {
		return
	}
	mid := (len(sides) - 1) / 2
	for y, row := range sides {
		// Clear the row's run of cells, so a line routed behind the box does
		// not show through it. Only the run: the frame's corners are outside
		// the shape, and a line crossing one is meant to be visible.
		for x := row.left; x <= row.right; x++ {
			g.clear(x0+x, y0+y)
		}
		// The sides run down the left of the box and back up its right, meeting
		// at the corners; where they meet at an apex the second one set wins,
		// which gives the top a / and the bottom a \.
		left, right := gl.forward, gl.back
		switch {
		case y == mid:
			left, right = '<', '>'
		case y > mid:
			left, right = gl.back, gl.forward
		}
		g.set(x0+row.right, y0+y, right, k)
		g.set(x0+row.left, y0+y, left, k)
		for i := 1; i <= row.tread; i++ {
			g.set(x0+row.left+i, y0+y, gl.h, k)
			g.set(x0+row.right-i, y0+y, gl.h, k)
		}
	}
}

// drawEdge draws a routed line and its arrow head.
//
// Mermaid's route is kept - the vertices it chose are what md draws through -
// but the curves between them become right angles. A terminal is a character
// grid, and a slanted line stepping through it one cell at a time reads as noise
// rather than as a line.
func drawEdge(g *grid, e Edge, nodes []Node, l layout, gl glyphs) {
	// Two cells of slack: enough to iron out the curve mermaid drew, not enough
	// to lose a corner it meant.
	pts := cellPoints(simplify(clipRoute(e.Points, nodes), 2*l.scale), l)
	if len(pts) < 2 {
		return
	}
	// The arrow head belongs just outside the target, or the box's own frame,
	// which is drawn afterwards, would cover it.
	if last, prev := pts[len(pts)-1], pts[len(pts)-2]; last != prev {
		last.x -= sign(last.x - prev.x)
		last.y -= sign(last.y - prev.y)
		pts[len(pts)-1] = last
	}
	for i := 1; i < len(pts); i++ {
		// Approaching the target, the line should arrive across then in, so
		// that the arrow meets the box square on rather than from the side.
		vertical := i != len(pts)-1
		g.leg(pts[i-1], pts[i], vertical, gl)
	}
	a, b := pts[len(pts)-2], pts[len(pts)-1]
	dx, dy := sign(b.x-a.x), sign(b.y-a.y)
	switch {
	case dx > 0:
		g.set(b.x, b.y, gl.arrowRight, KindEdge)
	case dx < 0:
		g.set(b.x, b.y, gl.arrowLeft, KindEdge)
	case dy > 0:
		g.set(b.x, b.y, gl.arrowDown, KindEdge)
	case dy < 0:
		g.set(b.x, b.y, gl.arrowUp, KindEdge)
	}
}

// cell is a position on the character grid.
type cell struct{ x, y int }

// cellPoints converts a route to cells, dropping steps that do not move.
func cellPoints(points []Point, l layout) []cell {
	out := make([]cell, 0, len(points))
	for _, p := range points {
		x, y := toCell(p.X, p.Y, l)
		if len(out) > 0 && out[len(out)-1] == (cell{x, y}) {
			continue
		}
		out = append(out, cell{x, y})
	}
	return out
}

// leg draws one leg of a route, turning a slanted leg into two right angles.
func (g *grid) leg(a, b cell, verticalFirst bool, gl glyphs) {
	switch {
	case a.x == b.x || a.y == b.y:
		g.segment(a.x, a.y, b.x, b.y, KindEdge, gl)
	case verticalFirst:
		g.segment(a.x, a.y, a.x, b.y, KindEdge, gl)
		g.segment(a.x, b.y, b.x, b.y, KindEdge, gl)
	default:
		g.segment(a.x, a.y, b.x, a.y, KindEdge, gl)
		g.segment(b.x, a.y, b.x, b.y, KindEdge, gl)
	}
}

// drawLabel centres a free-standing label, such as an edge label.
func drawLabel(g *grid, t Text, l layout) {
	if len(t.Label) == 0 {
		return
	}
	x, y := toCell(t.At.X, t.At.Y, l)
	lines := wrapCells(t.Label, max(g.w-2, 1))
	for i, text := range lines {
		left := x - xansi.StringWidth(text)/2
		if left < 0 {
			left = 0
		}
		g.text(left, y-len(lines)/2+i, text, KindDim)
	}
}

// toCell converts a diagram position to a cell.
func toCell(x, y float64, l layout) (int, int) {
	return int(x/l.scale + 0.5), int(y/(2*l.scale) + 0.5)
}

// lines renders the grid, styling runs of cells together.
func (g *grid) lines(style func(Kind, string) string) []string {
	if style == nil {
		style = func(_ Kind, text string) string { return text }
	}
	out := make([]string, 0, g.h)
	for y := 0; y < g.h; y++ {
		last := -1
		for x := g.w - 1; x >= 0; x-- {
			if g.ch[y*g.w+x] != 0 {
				last = x
				break
			}
		}
		if last < 0 {
			out = append(out, "")
			continue
		}
		var (
			sb  strings.Builder
			buf strings.Builder
			run = KindNone
		)
		flush := func() {
			if buf.Len() == 0 {
				return
			}
			sb.WriteString(style(run, buf.String()))
			buf.Reset()
		}
		for x := 0; x <= last; x++ {
			ch, k := g.ch[y*g.w+x], g.kind[y*g.w+x]
			if ch == 0 {
				ch, k = ' ', KindNone
			}
			if k != run {
				flush()
				run = k
			}
			buf.WriteRune(ch)
		}
		flush()
		out = append(out, sb.String())
	}
	// A drawing rarely fills its last rows: the diagram's coordinate space is
	// the box around everything mermaid placed, including empty space.
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	default:
		return 0
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

package mermaid

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Point is a position in diagram coordinates.
type Point struct{ X, Y float64 }

// Box is a rectangle in diagram coordinates.
type Box struct{ X, Y, W, H float64 }

// Center returns the middle of the box.
func (b Box) Center() Point { return Point{X: b.X + b.W/2, Y: b.Y + b.H/2} }

// matrix is a 2D affine transform in SVG's order: a b c d e f, where a point is
// mapped to (a*x+c*y+e, b*x+d*y+f).
type matrix struct{ a, b, c, d, e, f float64 }

// identity is the transform of an element with no transform attribute.
var identity = matrix{a: 1, d: 1}

// mul composes two transforms: the result applies o first, then m, which is what
// nesting one transform inside another does.
func (m matrix) mul(o matrix) matrix {
	return matrix{
		a: m.a*o.a + m.c*o.b,
		b: m.b*o.a + m.d*o.b,
		c: m.a*o.c + m.c*o.d,
		d: m.b*o.c + m.d*o.d,
		e: m.a*o.e + m.c*o.f + m.e,
		f: m.b*o.e + m.d*o.f + m.f,
	}
}

// apply maps a point through the transform.
func (m matrix) apply(p Point) Point {
	return Point{X: m.a*p.X + m.c*p.Y + m.e, Y: m.b*p.X + m.d*p.Y + m.f}
}

// box maps a rectangle through the transform. Mermaid only ever rotates nothing
// and scales uniformly, so mapping the corners is enough.
func (m matrix) box(b Box) Box {
	pts := []Point{
		m.apply(Point{X: b.X, Y: b.Y}),
		m.apply(Point{X: b.X + b.W, Y: b.Y}),
		m.apply(Point{X: b.X, Y: b.Y + b.H}),
		m.apply(Point{X: b.X + b.W, Y: b.Y + b.H}),
	}
	return bounds(pts)
}

// boxOfPoints returns the smallest box containing the points.
func (m matrix) boxOfPoints(pts []Point) Box {
	mapped := make([]Point, 0, len(pts))
	for _, p := range pts {
		mapped = append(mapped, m.apply(p))
	}
	return bounds(mapped)
}

func bounds(pts []Point) Box {
	if len(pts) == 0 {
		return Box{}
	}
	minX, minY := pts[0].X, pts[0].Y
	maxX, maxY := minX, minY
	for _, p := range pts[1:] {
		minX, maxX = math.Min(minX, p.X), math.Max(maxX, p.X)
		minY, maxY = math.Min(minY, p.Y), math.Max(maxY, p.Y)
	}
	return Box{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}
}

// parseTransform reads the transform attribute of an SVG element. Mermaid uses
// translate, occasionally scale, and matrix in its own layout output.
func parseTransform(s string) (matrix, error) {
	m := identity
	for _, fn := range transformCalls(s) {
		name, args, _ := strings.Cut(fn, "(")
		args = strings.TrimSuffix(args, ")")
		nums := numbers(args)
		switch strings.TrimSpace(name) {
		case "translate":
			switch len(nums) {
			case 1:
				m = m.mul(matrix{a: 1, d: 1, e: nums[0]})
			case 2:
				m = m.mul(matrix{a: 1, d: 1, e: nums[0], f: nums[1]})
			}
		case "scale":
			switch len(nums) {
			case 1:
				m = m.mul(matrix{a: nums[0], d: nums[0]})
			case 2:
				m = m.mul(matrix{a: nums[0], d: nums[1]})
			}
		case "matrix":
			if len(nums) == 6 {
				m = m.mul(matrix{a: nums[0], b: nums[1], c: nums[2], d: nums[3], e: nums[4], f: nums[5]})
			}
		default:
			return identity, fmt.Errorf("unsupported transform %q", name)
		}
	}
	return m, nil
}

// transformCalls splits "translate(1,2) scale(2)" into its calls.
func transformCalls(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '(':
			if depth == 0 {
				start = i
				for start > 0 && s[start-1] != ' ' {
					start--
				}
			}
			depth++
		case ')':
			depth--
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i+1]))
			}
		}
	}
	return out
}

// numbers extracts every number from a string.
func numbers(s string) []float64 {
	var out []float64
	for _, field := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}) {
		if v, err := strconv.ParseFloat(field, 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// flattenPath turns an SVG path into a polyline, sampling curves, which is what
// a node outline needs so that its bounding box is right.
func flattenPath(d string, m matrix) ([]Point, error) { return walkPath(d, m, true) }

// pathVertices returns only the points a path commands end at, which is what an
// edge needs: the vertices mermaid routed through, without the sampled curves
// in between. Edges are redrawn on a character grid, so the curve's shape is
// lost anyway and the vertices are the honest input.
func pathVertices(d string, m matrix) ([]Point, error) { return walkPath(d, m, false) }

// walkPath reads an SVG path. Only the commands mermaid emits are supported:
// anything else makes the path undrawable rather than approximated.
func walkPath(d string, m matrix, sample bool) ([]Point, error) {
	var (
		out     []Point
		cur     Point
		start   Point
		prevCtl Point
		lastCmd byte
	)
	toks := pathTokens(d)
	for i := 0; i < len(toks); {
		cmd := toks[i].cmd
		i++
		// take reads the next n numbers, stopping early if the command runs out
		// of arguments.
		take := func(n int) []float64 {
			args := make([]float64, 0, n)
			for len(args) < n && i < len(toks) && toks[i].isNum {
				args = append(args, toks[i].value)
				i++
			}
			return args
		}
		relative := cmd >= 'a' && cmd <= 'z'
		switch cmd | 0x20 { // lower-case
		case 'm':
			a := take(2)
			if len(a) < 2 {
				return nil, fmt.Errorf("bad moveto in %q", d)
			}
			if relative {
				cur.X += a[0]
				cur.Y += a[1]
			} else {
				cur = Point{X: a[0], Y: a[1]}
			}
			start = cur
			out = append(out, m.apply(cur))
		case 'l':
			a := take(2)
			if len(a) < 2 {
				return nil, fmt.Errorf("bad lineto in %q", d)
			}
			if relative {
				cur.X += a[0]
				cur.Y += a[1]
			} else {
				cur = Point{X: a[0], Y: a[1]}
			}
			out = append(out, m.apply(cur))
		case 'h':
			a := take(1)
			if len(a) < 1 {
				return nil, fmt.Errorf("bad horizontal line in %q", d)
			}
			if relative {
				cur.X += a[0]
			} else {
				cur.X = a[0]
			}
			out = append(out, m.apply(cur))
		case 'v':
			a := take(1)
			if len(a) < 1 {
				return nil, fmt.Errorf("bad vertical line in %q", d)
			}
			if relative {
				cur.Y += a[0]
			} else {
				cur.Y = a[0]
			}
			out = append(out, m.apply(cur))
		case 'c':
			a := take(6)
			if len(a) < 6 {
				return nil, fmt.Errorf("bad cubic curve in %q", d)
			}
			ctl1 := Point{X: a[0], Y: a[1]}
			ctl2 := Point{X: a[2], Y: a[3]}
			end := Point{X: a[4], Y: a[5]}
			if relative {
				ctl1 = add(cur, ctl1)
				ctl2 = add(cur, ctl2)
				end = add(cur, end)
			}
			out = append(out, bezier(m, cur, ctl1, ctl2, end)...)
			if !sample {
				out = append(out, m.apply(end))
			}
			cur, prevCtl = end, ctl2
			lastCmd = 'c'
		case 's':
			a := take(4)
			if len(a) < 4 {
				return nil, fmt.Errorf("bad smooth curve in %q", d)
			}
			ctl1 := cur
			if lastCmd == 'c' || lastCmd == 's' {
				ctl1 = Point{X: 2*cur.X - prevCtl.X, Y: 2*cur.Y - prevCtl.Y}
			}
			ctl2 := Point{X: a[0], Y: a[1]}
			end := Point{X: a[2], Y: a[3]}
			if relative {
				ctl2 = add(cur, ctl2)
				end = add(cur, end)
			}
			out = append(out, bezier(m, cur, ctl1, ctl2, end)...)
			if !sample {
				out = append(out, m.apply(end))
			}
			cur, prevCtl = end, ctl2
			lastCmd = 's'
		case 'q':
			a := take(4)
			if len(a) < 4 {
				return nil, fmt.Errorf("bad quadratic curve in %q", d)
			}
			ctl := Point{X: a[0], Y: a[1]}
			end := Point{X: a[2], Y: a[3]}
			if relative {
				ctl = add(cur, ctl)
				end = add(cur, end)
			}
			out = append(out, quadratic(m, cur, ctl, end)...)
			if !sample {
				out = append(out, m.apply(end))
			}
			cur, prevCtl = end, ctl
			lastCmd = 'q'
		case 't':
			a := take(2)
			if len(a) < 2 {
				return nil, fmt.Errorf("bad smooth quadratic in %q", d)
			}
			ctl := cur
			if lastCmd == 'q' || lastCmd == 't' {
				ctl = Point{X: 2*cur.X - prevCtl.X, Y: 2*cur.Y - prevCtl.Y}
			}
			end := Point{X: a[0], Y: a[1]}
			if relative {
				end = add(cur, end)
			}
			out = append(out, quadratic(m, cur, ctl, end)...)
			if !sample {
				out = append(out, m.apply(end))
			}
			cur, prevCtl = end, ctl
			lastCmd = 't'
		case 'a':
			// Flags are read as plain numbers: mermaid always separates them.
			a := take(7)
			if len(a) < 7 {
				return nil, fmt.Errorf("bad arc in %q", d)
			}
			end := Point{X: a[5], Y: a[6]}
			if relative {
				end = add(cur, end)
			}
			out = append(out, arcPoints(m, cur, a[0], a[1], a[2], a[3] != 0, a[4] != 0, end)...)
			if !sample {
				out = append(out, m.apply(end))
			}
			cur = end
		case 'z':
			out = append(out, m.apply(start))
			cur = start
		default:
			// An unsupported command (an arc, say). Treat the path as
			// undrawable rather than guessing at it.
			return nil, fmt.Errorf("unsupported path command %q", string(cmd))
		}
	}
	return out, nil
}

// pathToken is a command byte or a number from a path's d attribute.
type pathToken struct {
	cmd   byte
	value float64
	isNum bool
}

// pathTokens splits a path into commands and numbers, treating a minus sign and
// a second decimal point as implicit separators, the way SVG does.
func pathTokens(d string) []pathToken {
	var out []pathToken
	var num strings.Builder
	flush := func() {
		if num.Len() > 0 {
			if v, err := strconv.ParseFloat(num.String(), 64); err == nil {
				out = append(out, pathToken{value: v, isNum: true})
			}
			num.Reset()
		}
	}
	for i := 0; i < len(d); i++ {
		c := d[i]
		switch {
		case c == ' ' || c == ',' || c == '\n' || c == '\t' || c == '\r':
			flush()
		case c == '-' || c == '+':
			flush()
			num.WriteByte(c)
		case c == '.':
			if strings.Contains(num.String(), ".") {
				flush()
			}
			num.WriteByte(c)
		case (c >= '0' && c <= '9'):
			num.WriteByte(c)
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			flush()
			out = append(out, pathToken{cmd: c})
		}
	}
	flush()
	return out
}

// arcPoints samples an elliptical arc, following the endpoint to centre
// conversion in the SVG specification. Cylinders are drawn with arcs, and so are
// pie charts, so this is worth getting right rather than approximating with a
// straight line.
func arcPoints(m matrix, p0 Point, rx, ry, rotDeg float64, largeArc, sweep bool, p1 Point) []Point {
	if rx == 0 || ry == 0 || p0 == p1 {
		return []Point{m.apply(p1)}
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	phi := rotDeg * math.Pi / 180
	cos, sin := math.Cos(phi), math.Sin(phi)

	dx, dy := (p0.X-p1.X)/2, (p0.Y-p1.Y)/2
	x1 := cos*dx + sin*dy
	y1 := -sin*dx + cos*dy

	// Scale the radii up if they are too small to reach between the endpoints.
	if lambda := x1*x1/(rx*rx) + y1*y1/(ry*ry); lambda > 1 {
		s := math.Sqrt(lambda)
		rx, ry = s*rx, s*ry
	}

	num := rx*rx*ry*ry - rx*rx*y1*y1 - ry*ry*x1*x1
	den := rx*rx*y1*y1 + ry*ry*x1*x1
	coef := 0.0
	if den > 0 {
		coef = math.Sqrt(math.Max(0, num/den))
	}
	if largeArc == sweep {
		coef = -coef
	}
	cx1 := coef * rx * y1 / ry
	cy1 := -coef * ry * x1 / rx
	cx := cos*cx1 - sin*cy1 + (p0.X+p1.X)/2
	cy := sin*cx1 + cos*cy1 + (p0.Y+p1.Y)/2

	start := math.Atan2((y1-cy1)/ry, (x1-cx1)/rx)
	end := math.Atan2((-y1-cy1)/ry, (-x1-cx1)/rx)
	delta := end - start
	switch {
	case !sweep && delta > 0:
		delta -= 2 * math.Pi
	case sweep && delta < 0:
		delta += 2 * math.Pi
	}

	const steps = 24
	out := make([]Point, 0, steps)
	for i := 1; i <= steps; i++ {
		t := start + delta*float64(i)/steps
		ex, ey := rx*math.Cos(t), ry*math.Sin(t)
		out = append(out, m.apply(Point{X: cos*ex - sin*ey + cx, Y: sin*ex + cos*ey + cy}))
	}
	return out
}

// clipRoute trims a route to the boxes it connects.
//
// Mermaid anchors an edge where the node's *shape* is, which for a diamond is
// inside the box md knows about, so without this an edge appears to start in
// mid-air inside the node and its arrow lands inside the target too.
func clipRoute(pts []Point, nodes []Node) []Point {
	if len(pts) < 2 {
		return pts
	}
	if out, ok := clipEnd(pts, nodes); ok {
		pts = out
	}
	// The far end is clipped by walking the route backwards, so the same code
	// handles both ends.
	rev := make([]Point, 0, len(pts))
	for i := len(pts) - 1; i >= 0; i-- {
		rev = append(rev, pts[i])
	}
	if out, ok := clipEnd(rev, nodes); ok {
		pts = pts[:0]
		for i := len(out) - 1; i >= 0; i-- {
			pts = append(pts, out[i])
		}
	}
	return pts
}

// clipEnd drops the leading points that fall inside the box the route starts in,
// and moves the first remaining point onto the box's border.
func clipEnd(pts []Point, nodes []Node) ([]Point, bool) {
	box, ok := nodeAt(nodes, pts[0])
	if !ok {
		return pts, false
	}
	for i := 1; i < len(pts); i++ {
		if inside(box, pts[i]) {
			continue
		}
		if p, ok := segmentBoxExit(box, pts[i-1], pts[i]); ok {
			out := append([]Point{p}, pts[i:]...)
			return out, true
		}
		return pts[i:], true
	}
	return pts, false
}

// nodeAt returns the node a point belongs to: the smallest box containing it.
func nodeAt(nodes []Node, p Point) (Box, bool) {
	var best Box
	found := false
	for _, n := range nodes {
		if !inside(n.Box, p) {
			continue
		}
		if !found || n.Box.W*n.Box.H < best.W*best.H {
			best, found = n.Box, true
		}
	}
	return best, found
}

// inside reports whether a point is within a box.
func inside(b Box, p Point) bool {
	return p.X >= b.X && p.X <= b.X+b.W && p.Y >= b.Y && p.Y <= b.Y+b.H
}

// segmentBoxExit returns where a segment that starts inside a box leaves it.
func segmentBoxExit(box Box, a, b Point) (Point, bool) {
	dx, dy := b.X-a.X, b.Y-a.Y
	best := math.Inf(1)
	consider := func(t float64) {
		if t <= 0 || t >= 1 || t >= best {
			return
		}
		p := Point{X: a.X + t*dx, Y: a.Y + t*dy}
		if p.X >= box.X-0.01 && p.X <= box.X+box.W+0.01 &&
			p.Y >= box.Y-0.01 && p.Y <= box.Y+box.H+0.01 {
			best = t
		}
	}
	if dx != 0 {
		consider((box.X - a.X) / dx)
		consider((box.X + box.W - a.X) / dx)
	}
	if dy != 0 {
		consider((box.Y - a.Y) / dy)
		consider((box.Y + box.H - a.Y) / dy)
	}
	if math.IsInf(best, 1) {
		return Point{}, false
	}
	return Point{X: a.X + best*dx, Y: a.Y + best*dy}, true
}

func add(p, q Point) Point { return Point{X: p.X + q.X, Y: p.Y + q.Y} }

// simplify drops the points that sit close to the line between their neighbours,
// so the chain of short curves mermaid emits for one edge becomes the few
// corners worth drawing. Mermaid emits a chain of cubic segments rather than a
// single curve, so without this a route reads as a staircase.
func simplify(pts []Point, tol float64) []Point {
	if len(pts) < 3 {
		return pts
	}
	keep := make([]bool, len(pts))
	keep[0], keep[len(pts)-1] = true, true
	markSimplest(pts, 0, len(pts)-1, tol, keep)
	out := make([]Point, 0, len(pts))
	for i, p := range pts {
		if keep[i] {
			out = append(out, p)
		}
	}
	return out
}

// markSimplest is the recursive half of Douglas-Peucker: it keeps the point
// furthest from the line through the ends, then repeats on each side.
func markSimplest(pts []Point, first, last int, tol float64, keep []bool) {
	if last <= first+1 {
		return
	}
	worst, at := 0.0, -1
	for i := first + 1; i < last; i++ {
		if d := pointLineDistance(pts[i], pts[first], pts[last]); d > worst {
			worst, at = d, i
		}
	}
	if at < 0 || worst <= tol {
		return
	}
	keep[at] = true
	markSimplest(pts, first, at, tol, keep)
	markSimplest(pts, at, last, tol, keep)
}

// pointLineDistance is the distance from p to the segment ab.
func pointLineDistance(p, a, b Point) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	if dx == 0 && dy == 0 {
		return math.Hypot(p.X-a.X, p.Y-a.Y)
	}
	t := ((p.X-a.X)*dx + (p.Y-a.Y)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(p.X-(a.X+t*dx), p.Y-(a.Y+t*dy))
}

// union returns the smallest box containing both, treating an empty box as
// absent.
func union(a, b Box) Box {
	if a.W == 0 && a.H == 0 {
		return b
	}
	if b.W == 0 && b.H == 0 {
		return a
	}
	minX, minY := math.Min(a.X, b.X), math.Min(a.Y, b.Y)
	maxX := math.Max(a.X+a.W, b.X+b.W)
	maxY := math.Max(a.Y+a.H, b.Y+b.H)
	return Box{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}
}

// bezier samples a cubic curve, and quadratic samples a quadratic one: the grid
// is far coarser than the curve, so a fixed number of segments is plenty and
// keeps the result deterministic.
func bezier(m matrix, p0, p1, p2, p3 Point) []Point {
	const steps = 12
	out := make([]Point, 0, steps)
	for i := 1; i <= steps; i++ {
		t := float64(i) / steps
		u := 1 - t
		p := Point{
			X: u*u*u*p0.X + 3*u*u*t*p1.X + 3*u*t*t*p2.X + t*t*t*p3.X,
			Y: u*u*u*p0.Y + 3*u*u*t*p1.Y + 3*u*t*t*p2.Y + t*t*t*p3.Y,
		}
		out = append(out, m.apply(p))
	}
	return out
}

func quadratic(m matrix, p0, p1, p2 Point) []Point {
	const steps = 12
	out := make([]Point, 0, steps)
	for i := 1; i <= steps; i++ {
		t := float64(i) / steps
		u := 1 - t
		p := Point{
			X: u*u*p0.X + 2*u*t*p1.X + t*t*p2.X,
			Y: u*u*p0.Y + 2*u*t*p1.Y + t*t*p2.Y,
		}
		out = append(out, m.apply(p))
	}
	return out
}

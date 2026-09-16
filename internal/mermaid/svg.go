package mermaid

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// Diagram is the geometry md draws: where the boxes are, what they say, and how
// the lines between them run.
type Diagram struct {
	// Width and Height are the diagram's own coordinate space, in the units the
	// rest of the geometry uses.
	Width, Height float64
	Nodes         []Node
	Edges         []Edge
	Texts         []Text
}

// Node is one box in the diagram.
type Node struct {
	// Box is the box's outline, in diagram coordinates.
	Box Box
	// Shape is the outline to draw.
	Shape Shape
	// Label is the text in the box, already split into lines.
	Label []string
}

// Shape is how a node's outline is drawn.
type Shape int

const (
	// Rect is the default box.
	Rect Shape = iota
	// Diamond is a decision, drawn with pointed ends.
	Diamond
	// Round is a terminator, drawn with rounded corners.
	Round
)

// String names the shape, so that a test failure says which one came out.
func (s Shape) String() string {
	switch s {
	case Diamond:
		return "diamond"
	case Round:
		return "round"
	default:
		return "rect"
	}
}

// Edge is a routed line between two nodes.
type Edge struct {
	// Points is the line, flattened into a polyline, running from the start to
	// the end where the arrow head belongs.
	Points []Point
}

// Text is a label that stands on its own, such as an edge label.
type Text struct {
	// At is the point the label is centred on.
	At Point
	// Label is the text, already split into lines.
	Label []string
}

// ErrNoGeometry reports an SVG holding nothing this package can draw, which is
// the signal to show the fence as source instead.
var ErrNoGeometry = errors.New("no diagram geometry in the SVG")

// ErrNotSupported reports a diagram with parts this package cannot draw. Like
// ErrNoGeometry it means "show the source": a diagram missing a node would be a
// lie about the document.
var ErrNotSupported = errors.New("unsupported diagram")

// Parse reads the geometry out of an SVG document produced by mermaid.
//
// It is deliberately narrow: it knows the handful of shapes mermaid's flowchart
// renderer emits - node groups, the containers that size them, and the paths
// between them - and ignores everything else, so restyling cannot break it.
// Being lenient is safe precisely because the caller has somewhere to fall back
// to.
func Parse(r io.Reader) (Diagram, error) {
	p := &parser{trans: identity}
	dec := xml.NewDecoder(r)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Diagram{}, fmt.Errorf("reading SVG: %w", err)
		}
		if err := p.token(tok); err != nil {
			return Diagram{}, err
		}
	}
	if len(p.d.Nodes) == 0 {
		return Diagram{}, ErrNoGeometry
	}
	if p.unreadable > 0 {
		// A node md cannot place would go missing from the drawing, which is
		// worse than showing the source: a diagram that quietly omits a step
		// is a lie about the document.
		return Diagram{}, fmt.Errorf("%w: %d of %d nodes", ErrNotSupported, p.unreadable, p.groups)
	}
	return p.d, nil
}

// polygonShape tells the shapes mermaid draws as polygons apart.
//
// A polygon is not necessarily a diamond: a subroutine is one too, drawn as a
// rectangle with the two bars that mark it, and a hexagon is a third. What
// separates them is how much of the bounding box they fill. A diamond is a
// square turned on its corner and so fills half of it; a hexagon three quarters;
// a rectangle all of it. Anything fuller than a diamond is drawn as a box.
func polygonShape(pts []Point) Shape {
	box := bounds(pts)
	if box.W <= 0 || box.H <= 0 {
		return Rect
	}
	if math.Abs(shoelace(pts)) <= diamondFill*box.W*box.H {
		return Diamond
	}
	return Rect
}

// diamondFill is the fraction of its bounding box a diamond covers, with room
// for the rounding in mermaid's own coordinates.
const diamondFill = 0.6

// shoelace returns twice the signed area of a closed polygon, by the surveyor's
// formula. A polygon whose points retrace a shape - mermaid draws a subroutine's
// bars as part of the same loop - adds to the area rather than cancelling, which
// is what makes the fill test work on those too.
func shoelace(pts []Point) float64 {
	if len(pts) < 3 {
		return 0
	}
	sum := 0.0
	for i, p := range pts {
		q := pts[(i+1)%len(pts)]
		sum += p.X*q.Y - q.X*p.Y
	}
	return sum / 2
}

// captureKind records what an open group is contributing geometry for.
type captureKind int

const (
	kindNone captureKind = iota
	kindNode
	kindText
)

// frame is one open SVG element.
type frame struct {
	trans    matrix
	captured captureKind
}

type parser struct {
	d      Diagram
	trans  matrix
	frames []frame
	// node and text are the captures in progress, if any.
	node *Node
	text *Text
	// line accumulates the characters of the label line being read.
	line strings.Builder
	// groups counts the node groups seen and unreadable the ones that yielded no
	// shape, so a diagram with a node md cannot draw is refused rather than
	// drawn wrong.
	groups     int
	unreadable int
}

func (p *parser) token(tok xml.Token) error {
	switch t := tok.(type) {
	case xml.StartElement:
		return p.start(t)
	case xml.EndElement:
		return p.end(t)
	case xml.CharData:
		if p.capturing() {
			p.line.Write(t)
		}
	}
	return nil
}

// capturing reports whether a node or label is being read.
func (p *parser) capturing() bool { return p.node != nil || p.text != nil }

func (p *parser) start(el xml.StartElement) error {
	p.frames = append(p.frames, frame{trans: p.trans})
	if v := attr(el, "transform"); v != "" {
		m, err := parseTransform(v)
		if err != nil {
			return err
		}
		p.trans = p.trans.mul(m)
	}

	switch el.Name.Local {
	case "svg":
		p.d.Width, p.d.Height = viewBoxSize(el)
	case "g":
		class := attr(el, "class")
		switch {
		case p.capturing():
			// Inside a label: the markup here is the label's own.
		case hasClass(class, "node"):
			p.node = &Node{}
			p.groups++
			p.frames[len(p.frames)-1].captured = kindNode
		case hasClass(class, "edgeLabel"), hasClass(class, "cluster-label"):
			// The group's transform places the label; the offset inside it is
			// part of mermaid's own text metrics, which md does not use.
			p.text = &Text{At: p.trans.apply(Point{})}
			p.frames[len(p.frames)-1].captured = kindText
		}
	case "rect":
		if p.node != nil && p.node.Box.W == 0 && hasClass(attr(el, "class"), "label-container") {
			p.node.Box = p.trans.box(boxOf(el))
			if rx, err := strconv.ParseFloat(attr(el, "rx"), 64); err == nil && rx > 0 {
				p.node.Shape = Round
			}
		}
	case "polygon":
		if p.node != nil && p.node.Box.W == 0 {
			if pts, ok := pointsOf(attr(el, "points")); ok {
				p.node.Box = p.trans.boxOfPoints(pts)
				p.node.Shape = polygonShape(pts)
			}
		}
	case "circle":
		if p.node != nil && p.node.Box.W == 0 {
			r := floatAttr(el, "r")
			p.node.Box = p.trans.box(Box{
				X: floatAttr(el, "cx") - r,
				Y: floatAttr(el, "cy") - r,
				W: 2 * r,
				H: 2 * r,
			})
			p.node.Shape = Round
		}
	case "path":
		class := attr(el, "class")
		switch {
		case hasClass(class, "flowchart-link"):
			if pts, err := pathVertices(attr(el, "d"), p.trans); err == nil && len(pts) > 1 {
				p.d.Edges = append(p.d.Edges, Edge{Points: pts})
			}
		case p.node != nil:
			// Not every node is a rectangle: a stadium or a cylinder is drawn
			// as a path, so the outline comes from its bounding box.
			if pts, err := flattenPath(attr(el, "d"), p.trans); err == nil && len(pts) > 1 {
				p.node.Box = union(p.node.Box, bounds(pts))
				p.node.Shape = Round
			}
		}
	case "p", "tspan":
		// A new label line: mermaid writes one element per line.
		if p.capturing() {
			p.flush()
		}
	case "br":
		if p.capturing() {
			p.flush()
		}
	}
	return nil
}

func (p *parser) end(el xml.EndElement) error {
	switch el.Name.Local {
	case "p", "tspan", "text":
		if p.capturing() {
			p.flush()
		}
	}
	if len(p.frames) == 0 {
		return nil
	}
	top := len(p.frames) - 1
	switch p.frames[top].captured {
	case kindNode:
		p.commitNode()
	case kindText:
		p.commitText()
	}
	p.trans = p.frames[top].trans
	p.frames = p.frames[:top]
	return nil
}

// flush ends the label line being read and keeps it, if it said anything.
func (p *parser) flush() {
	text := collapseSpace(p.line.String())
	p.line.Reset()
	if text == "" {
		return
	}
	switch {
	case p.node != nil:
		p.node.Label = append(p.node.Label, text)
	case p.text != nil:
		p.text.Label = append(p.text.Label, text)
	}
}

// commitNode keeps the node just read, if it had a shape to draw.
func (p *parser) commitNode() {
	if p.node != nil {
		if p.node.Box.W > 0 && p.node.Box.H > 0 {
			p.d.Nodes = append(p.d.Nodes, *p.node)
		} else {
			p.unreadable++
		}
	}
	p.node = nil
	p.line.Reset()
}

// commitText keeps the label just read, if it said anything.
func (p *parser) commitText() {
	if p.text != nil && len(p.text.Label) > 0 {
		p.d.Texts = append(p.d.Texts, *p.text)
	}
	p.text = nil
	p.line.Reset()
}

func attr(el xml.StartElement, name string) string {
	for _, a := range el.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func floatAttr(el xml.StartElement, name string) float64 {
	v, _ := strconv.ParseFloat(attr(el, name), 64)
	return v
}

// hasClass reports whether a class attribute contains a token. Mermaid puts
// several on one element, such as "basic label-container".
func hasClass(class, want string) bool {
	for _, field := range strings.Fields(class) {
		if field == want {
			return true
		}
	}
	return false
}

func boxOf(el xml.StartElement) Box {
	return Box{
		X: floatAttr(el, "x"),
		Y: floatAttr(el, "y"),
		W: floatAttr(el, "width"),
		H: floatAttr(el, "height"),
	}
}

func pointsOf(s string) ([]Point, bool) {
	nums := numbers(s)
	if len(nums) < 4 {
		return nil, false
	}
	pts := make([]Point, 0, len(nums)/2)
	for i := 0; i+1 < len(nums); i += 2 {
		pts = append(pts, Point{X: nums[i], Y: nums[i+1]})
	}
	return pts, true
}

// viewBoxSize reads the diagram's coordinate space.
func viewBoxSize(el xml.StartElement) (float64, float64) {
	if nums := numbers(attr(el, "viewBox")); len(nums) == 4 {
		return nums[2], nums[3]
	}
	return floatAttr(el, "width"), floatAttr(el, "height")
}

func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

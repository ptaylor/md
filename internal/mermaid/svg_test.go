package mermaid

import (
	"math"
	"os"
	"strings"
	"testing"
)

func parseFixture(t *testing.T, name string) Diagram {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck // a test fixture
	d, err := Parse(f)
	if err != nil {
		t.Fatalf("parsing %s: %v", name, err)
	}
	return d
}

// TestParseMinimal checks the parser against a hand-written document with the
// shape mermaid uses, so a failure points at the parser rather than a fixture.
func TestParseMinimal(t *testing.T) {
	const doc = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
  <g class="root">
    <g class="nodes">
      <g class="node default" transform="translate(30, 20)">
        <rect class="basic label-container" x="-20" y="-10" width="40" height="20"/>
        <g class="label"><rect/>
          <foreignObject width="10" height="10"><div><span class="nodeLabel"><p>hello</p></span></div></foreignObject>
        </g>
      </g>
    </g>
    <g class="edgeLabels">
      <g class="edgeLabel" transform="translate(50, 60)">
        <g class="label"><foreignObject><div><span class="edgeLabel"><p>yes</p></span></div></foreignObject></g>
      </g>
    </g>
  </g>
</svg>`

	d, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(d.Nodes), 1; got != want {
		t.Fatalf("nodes = %d, want %d", got, want)
	}
	if got, want := d.Nodes[0].Box, (Box{X: 10, Y: 10, W: 40, H: 20}); got != want {
		t.Errorf("node box = %+v, want %+v", got, want)
	}
	if got, want := strings.Join(d.Nodes[0].Label, "|"), "hello"; got != want {
		t.Errorf("node label = %q, want %q", got, want)
	}
	if got, want := len(d.Texts), 1; got != want {
		t.Fatalf("texts = %d, want %d", got, want)
	}
	if got, want := d.Texts[0].At, (Point{X: 50, Y: 60}); got != want {
		t.Errorf("label at = %+v, want %+v", got, want)
	}
	if got, want := strings.Join(d.Texts[0].Label, "|"), "yes"; got != want {
		t.Errorf("label = %q, want %q", got, want)
	}
}

// TestParseFixtures reads the geometry out of real mmdc output. The fixtures are
// committed so that the tests never need mmdc installed.
func TestParseFixtures(t *testing.T) {
	for _, tc := range []struct {
		file   string
		nodes  int
		edges  int
		labels []string
	}{
		{"flowchart_td.svg", 5, 5, []string{"md README.md", "terminal has colour?"}},
		{"flowchart_lr.svg", 4, 3, []string{"read file", "page"}},
		{"shapes.svg", 5, 5, []string{"start", "cache", "retry"}},
		{"subgraph.svg", 3, 2, []string{"goldmark", "panels"}},
		{"unicode.svg", 3, 2, []string{"café", "日本語のラベル"}},
	} {
		t.Run(tc.file, func(t *testing.T) {
			d := parseFixture(t, tc.file)
			if got := len(d.Nodes); got != tc.nodes {
				t.Errorf("nodes = %d, want %d", got, tc.nodes)
			}
			if got := len(d.Edges); got != tc.edges {
				t.Errorf("edges = %d, want %d", got, tc.edges)
			}
			for _, want := range tc.labels {
				if !hasLabel(d, want) {
					t.Errorf("no node labelled %q", want)
				}
			}
			if d.Width <= 0 || d.Height <= 0 {
				t.Errorf("diagram is %vx%v, want a real coordinate space", d.Width, d.Height)
			}
		})
	}
}

// TestParsePolygonShapesAreToldApart checks that a diamond is recognised from its
// geometry rather than from the fact that mermaid happens to draw it as a
// polygon. A subroutine is a polygon too, and taking one for a diamond put its
// label into a shape with no room for it: the label wrapped to lines the drawing
// then dropped, so a node read "n" where it should have read "run".
func TestParsePolygonShapesAreToldApart(t *testing.T) {
	for _, tc := range []struct {
		name string
		pts  string
		box  Box
		want Shape
	}{
		{
			// A square standing on its corner, covering half its box.
			name: "diamond",
			pts:  "99.46,0 198.92,-99.46 99.46,-198.92 0,-99.46",
			box:  Box{X: 100, Y: 1.08, W: 198.92, H: 198.92},
			want: Diamond,
		},
		{
			// The bars mermaid draws across a subroutine are part of the same
			// loop, so the polygon covers more ground than its box: the outline
			// is the box, and the box is what md draws.
			name: "subroutine",
			pts:  "0,0 39,0 39,-39 0,-39 0,0 -8,0 47,0 47,-39 -8,-39 -8,0",
			box:  Box{X: 92, Y: 161, W: 55, H: 39},
			want: Rect,
		},
		{
			// A hexagon fills three quarters of its box, which is fuller than a
			// diamond: a box is a better lie than a diamond with clipped sides.
			name: "hexagon",
			pts:  "9.75,0 50.23,0 60,-19.5 50.23,-39 9.75,-39 0,-19.5",
			box:  Box{X: 100, Y: 161, W: 60, H: 39},
			want: Rect,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 300 300">
  <g class="nodes"><g class="node default" transform="translate(100, 200)">
    <polygon class="label-container" points="` + tc.pts + `"/>
    <g class="label"><foreignObject><div><span class="nodeLabel"><p>x</p></span></div></foreignObject></g>
  </g></g>
</svg>`
			d, err := Parse(strings.NewReader(doc))
			if err != nil {
				t.Fatal(err)
			}
			if got, want := d.Nodes[0].Shape, tc.want; got != want {
				t.Errorf("shape = %v, want %v", got, want)
			}
			if got := d.Nodes[0].Box; !sameBox(got, tc.box, 0.01) {
				t.Errorf("box = %+v, want %+v", got, tc.box)
			}
		})
	}
}

// sameBox compares boxes to within mermaid's own rounding.
func sameBox(a, b Box, tol float64) bool {
	close := func(x, y float64) bool { return math.Abs(x-y) <= tol }
	return close(a.X, b.X) && close(a.Y, b.Y) && close(a.W, b.W) && close(a.H, b.H)
}

func hasLabel(d Diagram, want string) bool {
	for _, n := range d.Nodes {
		if strings.Join(n.Label, " ") == want {
			return true
		}
	}
	return false
}

// TestParseRefusesDiagramsItCannotDraw is the safety property: a diagram with a
// node md cannot place is refused outright, because a drawing that quietly drops
// a step is a lie about the document.
func TestParseRefusesDiagramsItCannotDraw(t *testing.T) {
	const doc = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
  <g class="nodes">
    <g class="node default" transform="translate(10, 10)">
      <rect class="basic label-container" x="-5" y="-5" width="10" height="10"/>
      <g class="label"><foreignObject><div><span class="nodeLabel"><p>fine</p></span></div></foreignObject></g>
    </g>
    <g class="node default" transform="translate(50, 50)">
      <bogus x="1"/>
      <g class="label"><foreignObject><div><span class="nodeLabel"><p>mystery</p></span></div></foreignObject></g>
    </g>
  </g>
</svg>`

	if _, err := Parse(strings.NewReader(doc)); err == nil {
		t.Fatal("want an error for a node with no shape, got none")
	}
}

// TestParseRejectsGeometrylessSVG covers the other refusal: an SVG with nothing
// this package understands, such as a diagram type it cannot draw yet.
func TestParseRejectsGeometrylessSVG(t *testing.T) {
	const doc = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">
  <g><text x="1" y="1">hello</text></g>
</svg>`

	if _, err := Parse(strings.NewReader(doc)); err == nil {
		t.Fatal("want an error for an SVG with no nodes, got none")
	}
}

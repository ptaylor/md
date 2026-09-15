package render

import (
	"bytes"
	"context"

	"charm.land/lipgloss/v2"

	"github.com/pftylr/md/internal/mermaid"
	"github.com/pftylr/md/internal/theme"
)

// Diagrammer draws a document's mermaid diagrams as lines of terminal text. Each
// result corresponds to a source, and a nil result means that one could not be
// drawn, in which case md shows its source instead.
//
// The whole document is drawn at once rather than fence by fence: rendering a
// diagram starts a browser, so a document with several diagrams is worth
// starting them together.
type Diagrammer interface {
	Diagrams(sources []string, width int) [][]string
}

// Diagrams draws mermaid diagrams with mmdc.
//
// md reads the geometry mmdc produces and draws it itself, in the terminal's own
// colours, so that a diagram scrolls with the document and looks like part of
// the page - which an image inside a pager cannot do.
type Diagrams struct {
	tool    *mermaid.Tool
	palette theme.Palette
	ascii   bool
}

// NewDiagrams returns a diagrammer that draws with the given tool.
func NewDiagrams(tool *mermaid.Tool, palette theme.Palette, ascii bool) *Diagrams {
	return &Diagrams{tool: tool, palette: palette, ascii: ascii}
}

// Available reports whether diagrams can be drawn at all, which is what decides
// whether the source panel is a fallback or simply the way things look.
func (d *Diagrams) Available() bool { return d != nil && d.tool != nil && d.tool.Available() }

// Failures returns the diagrams that could not be drawn, so the caller can say
// so once rather than leaving the reader wondering.
func (d *Diagrams) Failures() []error {
	if d == nil || d.tool == nil {
		return nil
	}
	return d.tool.Failures()
}

// Diagrams draws each source at the given width, leaving a nil entry for any
// that cannot be drawn.
func (d *Diagrams) Diagrams(sources []string, width int) [][]string {
	out := make([][]string, len(sources))
	if !d.Available() || width < minPanelWidth || len(sources) == 0 {
		return out
	}
	svgs := d.tool.Diagrams(context.Background(), sources)
	for i, svg := range svgs {
		if len(svg) == 0 {
			continue
		}
		diagram, err := mermaid.Parse(bytes.NewReader(svg))
		if err != nil {
			continue
		}
		if lines, ok := mermaid.Render(diagram, mermaid.Options{
			Width: width,
			Ascii: d.ascii,
			Style: d.style,
		}); ok {
			out[i] = lines
		}
	}
	return out
}

// style colours a run of the drawing from the terminal's palette. A nil colour
// means the terminal is not drawing colours at all, so the text is left alone
// rather than wrapped in escape sequences that mean nothing.
func (d *Diagrams) style(kind mermaid.Kind, text string) string {
	var colour *string
	switch kind {
	case mermaid.KindBorder:
		colour = d.palette.Rule()
	case mermaid.KindEdge:
		colour = d.palette.Rule()
	case mermaid.KindText:
		colour = d.palette.Foreground()
	case mermaid.KindDim:
		colour = d.palette.Dim()
	}
	if colour == nil {
		return text
	}
	return lipgloss.NewStyle().Foreground(theme.Color(colour)).Render(text)
}

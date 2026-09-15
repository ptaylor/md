// Package render turns Markdown into styled, terminal-ready text.
//
// Rendering is a two-stage pipeline: goldmark parses the document and glamour's
// ANSI renderer styles it. md wires the two together itself rather than using
// glamour's own TermRenderer, for two reasons:
//
//   - glamour's TermRenderer hard-codes which goldmark extensions are enabled,
//     and md wants footnotes, definition lists and emoji.
//   - md wants to own the node kinds that need more than a colour change:
//     fenced code blocks become labelled panels, and mermaid diagrams become a
//     framed source block.
//
// Everything else - headings, lists, tables, block quotes - is glamour's,
// restyled to the terminal's own palette.
package render

import (
	"bytes"
	"fmt"
	"strings"

	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"github.com/alecthomas/chroma/v2"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/colorprofile"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	emoji "github.com/yuin/goldmark-emoji"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"

	"github.com/pftylr/md/internal/term"
	"github.com/pftylr/md/internal/theme"
)

const (
	// docMargin matches glamour's document margin: the two cells that indent
	// the whole document.
	docMargin = 2
	// levelIndent is how many cells each level of list or block quote nesting
	// adds, used to place md's own panels inside nested blocks.
	levelIndent = 2
	// minPanelWidth stops md's panels from collapsing on narrow terminals.
	minPanelWidth = 24
	// chromaStyleName is the chroma style md registers from the palette.
	chromaStyleName = "md"
)

// Options are the rendering choices that are not derived from the terminal.
type Options struct {
	// MaxWidth caps the content width so long lines stay readable on wide
	// terminals.
	MaxWidth int
	// Mermaid controls fenced mermaid blocks: "box" frames the source, "off"
	// renders them as ordinary code blocks.
	Mermaid string
	// Links controls link rendering: "auto", "inline", "both" or "plain".
	Links string
	// Ascii avoids non-ASCII glyphs entirely.
	Ascii bool
}

// Renderer renders Markdown for one terminal.
type Renderer struct {
	caps term.Caps
	opts Options
	// width is the content width of the render in progress. Panels that md
	// draws itself need it, and goldmark's renderer interface gives no way to
	// thread it through to a node renderer.
	width int
}

// New returns a Renderer for the given terminal capabilities.
func New(caps term.Caps, opts Options) *Renderer {
	r := &Renderer{caps: caps, opts: opts}
	r.registerChromaStyle()
	return r
}

// Width returns the content width for a terminal of the given total width: the
// space left after the document margin, capped by MaxWidth.
func (r *Renderer) Width(total int) int {
	w := total - docMargin - 1
	if r.opts.MaxWidth > 0 && w > r.opts.MaxWidth {
		w = r.opts.MaxWidth
	}
	if w < minPanelWidth {
		w = minPanelWidth
	}
	return w
}

// Render renders a whole document at the given terminal width.
func (r *Renderer) Render(src string, width int) (string, error) {
	body, front := splitFrontMatter(src)
	body, notes := extractFootnotes(body)
	body, blocks := substituteBlocks(body)
	wrap := r.Width(width)
	r.width = wrap

	ansiOpts := ansi.Options{
		WordWrap:         wrap,
		TableWrap:        boolPtr(true),
		InlineTableLinks: true, // keep links inside tables instead of listing them below
		Styles:           r.styleConfig(wrap),
		ChromaFormatter:  r.chromaFormatter(),
	}
	an := ansi.NewRenderer(ansiOpts)

	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM, // tables, strikethrough, autolinks, task lists
			extension.DefinitionList,
			// Footnotes are deliberately not enabled: glamour has no renderer
			// for those node kinds and prints a warning to stdout instead.
			// md extracts them from the source itself; see extractFootnotes.
			emoji.Emoji,
		),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)
	md.SetRenderer(renderer.NewRenderer(renderer.WithNodeRenderers(
		util.Prioritized(an, 1000),
	)))

	var buf bytes.Buffer
	if err := md.Convert([]byte(body), &buf); err != nil {
		return "", fmt.Errorf("rendering markdown: %w", err)
	}

	out := r.expandBlocks(buf.String(), blocks)
	out = r.applyLinkMode(out)
	if section := r.footnotesSection(notes); section != "" {
		out += section
	}
	if panel := r.frontMatterPanel(front, wrap); panel != "" {
		out = panel + out
	}
	if r.mono() {
		// Colour is off, so no escape sequence has any meaning. Stripping here
		// catches the attributes glamour adds on its own - bold headings and
		// strong text - as well as the hyperlinks wrapped around URLs.
		out = xansi.Strip(out)
	}
	return out, nil
}

// styleConfig builds the glamour style for this render.
//
// It starts from glamour's dark or light defaults so every structural detail
// (bullet glyphs, indent tokens, table separators, spacing) is preserved - a
// zero-value style has no structure at all - and then replaces every colour
// with either a value read from the terminal's own palette or nil, which means
// "whatever the terminal uses by default".
func (r *Renderer) styleConfig(wrap int) ansi.StyleConfig {
	if r.opts.Ascii {
		return r.asciiStyle(wrap)
	}
	s := styles.DarkStyleConfig
	if !r.caps.Dark {
		s = styles.LightStyleConfig
	}
	p := r.caps.Palette
	dim, rule := p.Dim(), p.Rule()

	s.Document = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockPrefix: "\n",
			BlockSuffix: "\n",
			Color:       p.Foreground(),
		},
		Margin: uintPtr(docMargin),
	}
	// Quotes are marked by their rule glyph rather than a colour, so that the
	// body text keeps the terminal's own foreground.
	s.BlockQuote = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{Color: p.Foreground()},
		Indent:         uintPtr(1),
		IndentToken:    strPtr("▌ "),
	}
	s.Paragraph = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: p.Foreground()}}
	s.List = ansi.StyleList{
		StyleBlock:  ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: p.Foreground()}},
		LevelIndent: levelIndent,
	}

	s.Heading = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockSuffix: "\n",
			Color:       p.Heading(),
			Bold:        boolPtr(true),
		},
	}
	s.H1 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{
		BlockPrefix:     "\n",
		Prefix:          " ",
		Suffix:          " ",
		Color:           p.Heading(),
		BackgroundColor: p.Panel(),
		Bold:            boolPtr(true),
	}}
	s.H2 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "▌ "}}
	s.H3 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "┃ "}}
	s.H4 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "│ ", Color: dim, Bold: boolPtr(false)}}
	s.H5 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "┆ ", Color: dim, Bold: boolPtr(false)}}
	s.H6 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "┊ ", Color: dim, Bold: boolPtr(false)}}

	s.Text = ansi.StylePrimitive{Color: p.Foreground()}
	s.Emph = ansi.StylePrimitive{} // no italics: several terminals render them as reverse video
	s.Strong = ansi.StylePrimitive{Bold: boolPtr(true), Color: p.Foreground()}
	s.Strikethrough = ansi.StylePrimitive{CrossedOut: boolPtr(true), Color: dim}
	s.HorizontalRule = ansi.StylePrimitive{
		Color:  rule,
		Format: "\n" + strings.Repeat("─", ruleWidth(wrap)) + "\n",
	}

	s.Item = ansi.StylePrimitive{BlockPrefix: "• ", Color: p.Accent()}
	s.Enumeration = ansi.StylePrimitive{BlockPrefix: ". ", Color: p.Accent()}
	s.Task = ansi.StyleTask{
		StylePrimitive: ansi.StylePrimitive{Color: p.Accent()},
		Ticked:         "[✓] ",
		Unticked:       "[ ] ",
	}

	s.Link = ansi.StylePrimitive{Color: p.Link(), Underline: boolPtr(true)}
	s.LinkText = ansi.StylePrimitive{Color: p.Link()}
	s.Image = ansi.StylePrimitive{Color: p.Link()}
	s.ImageText = ansi.StylePrimitive{Color: dim, Format: "🖼  {{.text}}"}

	s.Code = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{
		Prefix:          "\u00a0",
		Suffix:          "\u00a0",
		Color:           p.Accent(),
		BackgroundColor: p.Panel(),
	}}
	// md renders fenced code itself, so what glamour sees is only a sentinel
	// line per block. Indented code blocks have no sentinel and fall through to
	// glamour's own rendering; a zero margin keeps them aligned with the
	// document's own left margin.
	s.CodeBlock = ansi.StyleCodeBlock{
		StyleBlock: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: p.Foreground()},
			Margin:         uintPtr(0),
		},
	}

	s.Table = ansi.StyleTable{
		StyleBlock: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: p.Foreground()},
			Margin:         uintPtr(0),
		},
		CenterSeparator: strPtr("┼"),
		ColumnSeparator: strPtr("│"),
		RowSeparator:    strPtr("─"),
	}

	s.DefinitionList = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: p.Foreground()}}
	s.DefinitionTerm = ansi.StylePrimitive{Color: p.Heading(), Bold: boolPtr(true)}
	s.DefinitionDescription = ansi.StylePrimitive{Color: p.Foreground(), BlockPrefix: "\n    "}

	s.HTMLBlock = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: dim}}
	s.HTMLSpan = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: dim}}
	return s
}

// asciiStyle returns the style used for terminals or locales that cannot be
// trusted with box drawing characters.
//
// It starts from glamour's ASCII style, which is not entirely ASCII - its list
// bullets are still "•" - so the parts that matter are replaced here.
func (r *Renderer) asciiStyle(wrap int) ansi.StyleConfig {
	s := styles.ASCIIStyleConfig
	s.List = ansi.StyleList{
		StyleBlock:  ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{}},
		LevelIndent: levelIndent,
	}
	s.Item = ansi.StylePrimitive{BlockPrefix: "- "}
	s.Enumeration = ansi.StylePrimitive{BlockPrefix: ". "}
	s.Task = ansi.StyleTask{Ticked: "[x] ", Unticked: "[ ] "}
	s.HorizontalRule = ansi.StylePrimitive{
		Format: "\n" + strings.Repeat(r.ruleGlyph(), ruleWidth(wrap)) + "\n",
	}
	s.ImageText = ansi.StylePrimitive{Format: "Image: {{.text}}"}
	s.Code = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{BlockPrefix: "`", BlockSuffix: "`"}}
	return s
}

// chromaFormatter picks the chroma output formatter for the terminal's colour
// profile. An empty result means syntax highlighting is skipped entirely.
//
// The profile matters: terminal16m emits the exact palette values, terminal256
// approximates them, and terminal16 emits ANSI slot escapes that the terminal
// paints with its own colours.
func (r *Renderer) chromaFormatter() string {
	if r.opts.Ascii || r.mono() {
		return ""
	}
	switch {
	case r.caps.Profile >= colorprofile.TrueColor:
		return "terminal16m"
	case r.caps.Profile == colorprofile.ANSI256:
		return "terminal256"
	case r.caps.Profile == colorprofile.ANSI:
		return "terminal16"
	default:
		return ""
	}
}

// registerChromaStyle publishes the palette as a chroma style.
//
// Chroma cannot express ANSI colour indices, so code colours are always real
// RGB values: either the ones read back from the terminal, or the xterm
// defaults. The formatter chosen in chromaFormatter decides how faithfully
// those values come out.
func (r *Renderer) registerChromaStyle() {
	p := r.caps.Palette
	hex := p.HexSlot
	bold := func(i int) string { return "bold " + hex(i) }
	italic := func(i int) string { return "italic " + hex(i) }

	chromastyles.Register(chroma.MustNewStyle(chromaStyleName, chroma.StyleEntries{
		chroma.Text:                hex(theme.White),
		chroma.Error:               hex(theme.BrightRed),
		chroma.Comment:             hex(theme.BrightBlack),
		chroma.CommentPreproc:      hex(theme.BrightBlack),
		chroma.Keyword:             hex(theme.BrightMagenta),
		chroma.KeywordReserved:     hex(theme.BrightMagenta),
		chroma.KeywordNamespace:    hex(theme.BrightMagenta),
		chroma.KeywordType:         hex(theme.BrightYellow),
		chroma.Operator:            hex(theme.BrightCyan),
		chroma.Punctuation:         hex(theme.BrightBlack),
		chroma.Name:                hex(theme.White),
		chroma.NameBuiltin:         hex(theme.BrightCyan),
		chroma.NameTag:             hex(theme.BrightRed),
		chroma.NameAttribute:       hex(theme.BrightYellow),
		chroma.NameClass:           hex(theme.BrightYellow),
		chroma.NameConstant:        hex(theme.BrightCyan),
		chroma.NameDecorator:       hex(theme.BrightBlue),
		chroma.NameException:       hex(theme.BrightRed),
		chroma.NameFunction:        hex(theme.BrightBlue),
		chroma.NameOther:           hex(theme.White),
		chroma.Literal:             hex(theme.BrightGreen),
		chroma.LiteralNumber:       hex(theme.BrightMagenta),
		chroma.LiteralDate:         hex(theme.BrightGreen),
		chroma.LiteralString:       hex(theme.BrightGreen),
		chroma.LiteralStringEscape: hex(theme.BrightCyan),
		chroma.GenericDeleted:      hex(theme.BrightRed),
		chroma.GenericEmph:         italic(theme.White),
		chroma.GenericInserted:     bold(theme.BrightGreen),
		chroma.GenericStrong:       bold(theme.White),
		chroma.GenericSubheading:   hex(theme.BrightCyan),
		chroma.Background:          hex(theme.White),
	}))
}

func boolPtr(b bool) *bool    { return &b }
func uintPtr(u uint) *uint    { return &u }
func strPtr(s string) *string { return &s }

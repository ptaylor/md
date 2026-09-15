package render

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2/quick"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/pftylr/md/internal/theme"
)

// codeBlock records a fenced block whose content md renders itself.
type codeBlock struct {
	// id is the sentinel md leaves in the document for this block.
	id string
	// lang is the fence's info string, used as the panel label and to pick a
	// syntax highlighter.
	lang string
	// code is the block's content, with any quote or list prefix removed.
	code string
}

// substituteBlocks replaces the content of every fenced code block with a
// single sentinel line, keeping the fence and its info string intact.
//
// md draws code and mermaid panels itself, but glamour buffers the whole
// document and flushes it only when the document ends, so anything written
// straight to the output while walking the tree lands *above* the entire
// document. Instead, glamour renders a one-line sentinel exactly where the block
// belongs, and expandBlocks swaps that line for the finished panel afterwards.
// That keeps the document order, and because the sentinel inherits the block's
// indentation, it also keeps nested blocks - inside lists and quotes - in place.
//
// A block whose fences are not found is left untouched; it then renders as an
// ordinary code block rather than disappearing.
func substituteBlocks(src string) (string, []codeBlock) {
	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	var blocks []codeBlock

	for i := 0; i < len(lines); i++ {
		prefix := leadingPrefix(lines[i])
		marker := fenceMarker(strings.TrimPrefix(lines[i], prefix))
		if marker == "" {
			out = append(out, lines[i])
			continue
		}

		closeAt := -1
		for j := i + 1; j < len(lines); j++ {
			if leadingPrefix(lines[j]) != prefix {
				continue // content line, not a closing fence at this depth
			}
			if isClosingFence(strings.TrimPrefix(lines[j], prefix), marker) {
				closeAt = j
				break
			}
		}
		if closeAt < 0 {
			out = append(out, lines[i]) // unterminated fence: leave it alone
			continue
		}

		codeLines := make([]string, 0, closeAt-i-1)
		for _, line := range lines[i+1 : closeAt] {
			codeLines = append(codeLines, strings.TrimPrefix(line, prefix))
		}
		code := strings.Join(codeLines, "\n")
		lang := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(lines[i], prefix), marker))

		id := blockID(len(blocks), code)
		blocks = append(blocks, codeBlock{id: id, lang: lang, code: code})

		out = append(out, lines[i], prefix+id, lines[closeAt])
		i = closeAt
	}
	return strings.Join(out, "\n"), blocks
}

// expandBlocks replaces each rendered sentinel line with the panel it stands
// for. Ordering, and the indentation of nested blocks, come from the rendered
// document itself.
func (r *Renderer) expandBlocks(out string, blocks []codeBlock) string {
	if len(blocks) == 0 {
		return out
	}
	byID := make(map[string]codeBlock, len(blocks))
	for _, b := range blocks {
		byID[b.id] = b
	}

	lines := strings.Split(out, "\n")
	res := make([]string, 0, len(lines))
	for _, line := range lines {
		blk, ok := sentinel(line, byID)
		if !ok {
			res = append(res, line)
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		avail := r.width - max(indent-docMargin, 0)

		if isMermaid(blk.lang) && !strings.EqualFold(r.opts.Mermaid, "off") {
			res = append(res, r.panel("mermaid", r.dimLines(splitLines(expandTabs(blk.code))), indent, avail)...)
			continue
		}
		res = append(res, r.panel(blk.lang, r.highlight(blk.code, blk.lang), indent, avail)...)
	}
	return strings.Join(res, "\n")
}

// sentinel reports which code block a rendered line stands for, ignoring the
// styling and padding glamour wrapped around it.
func sentinel(line string, byID map[string]codeBlock) (codeBlock, bool) {
	for _, field := range strings.Fields(xansi.Strip(line)) {
		if blk, ok := byID[field]; ok {
			return blk, true
		}
	}
	return codeBlock{}, false
}

// isMermaid reports whether a fence's info string asks for a mermaid diagram.
func isMermaid(lang string) bool {
	fields := strings.Fields(lang)
	return len(fields) > 0 && strings.EqualFold(fields[0], "mermaid")
}

// highlight returns a code block's lines, syntax highlighted when the terminal
// can show colour and a language was given.
func (r *Renderer) highlight(code, lang string) []string {
	code = expandTabs(code)
	if formatter := r.chromaFormatter(); formatter != "" && lang != "" {
		var buf bytes.Buffer
		if err := quick.Highlight(&buf, code, lang, formatter, chromaStyleName); err == nil {
			return dropTrailingEmpty(splitLines(buf.String()))
		}
	}
	return dropTrailingEmpty(splitLines(code))
}

// dimLines renders lines in the palette's muted colour.
func (r *Renderer) dimLines(lines []string) []string {
	if r.mono() {
		return lines
	}
	style := lipgloss.NewStyle().Foreground(theme.Color(r.caps.Palette.Dim()))
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, r.paint(style, l))
	}
	return out
}

// blockID builds the sentinel left in the document for a code block. It has to
// survive Markdown and syntax highlighting intact, so it uses only letters,
// digits and hyphens, and carries a content hash so the same document always
// produces the same output.
func blockID(n int, code string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(code))
	return fmt.Sprintf("mdblock%d-%08x", n, h.Sum32())
}

// fenceMarker returns the fence marker a line consists of, if any. The line
// must already have any quote or list prefix removed.
func fenceMarker(line string) string {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 {
		return "" // indented code, not a fence
	}
	for _, marker := range []string{"```", "~~~"} {
		if strings.HasPrefix(trimmed, marker) {
			return marker
		}
	}
	return ""
}

// isClosingFence reports whether a line closes a fenced block opened with
// marker.
func isClosingFence(line, marker string) bool {
	trimmed := strings.TrimLeft(line, " ")
	if !strings.HasPrefix(trimmed, marker) {
		return false
	}
	return strings.Trim(trimmed, "`~ \t") == ""
}

// leadingPrefix returns the run of whitespace and quote markers that a line
// starts with, which is what a nested block's content has to be re-prefixed
// with.
func leadingPrefix(line string) string {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t' || line[i] == '>') {
		i++
	}
	return line[:i]
}

// isIndented reports whether a line is indented enough to continue a footnote
// definition.
func isIndented(line string) bool {
	return strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t")
}

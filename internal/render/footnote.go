package render

import (
	"regexp"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/pftylr/md/internal/theme"
)

var (
	// footnoteDef matches a footnote definition at the start of a line.
	footnoteDef = regexp.MustCompile(`^\[\^([^\]\s]+)\]:[ \t]*(.*)$`)
	// footnoteRef matches a footnote reference.
	footnoteRef = regexp.MustCompile(`\[\^([^\]\s]+)\]`)
)

// footnote is one collected note.
type footnote struct {
	// number is the marker shown in the text, assigned by order of first
	// reference.
	number int
	// label is the author's label, used to match references to definitions.
	label string
	// text is the note's content, flattened onto one line.
	text string
}

// extractFootnotes pulls footnote definitions out of a document, numbers the
// references that remain and returns the source to render plus the notes in the
// order they are first referenced.
//
// md handles footnotes itself rather than enabling goldmark's footnote
// extension, for two reasons discovered the hard way:
//
//   - glamour v2 renders node kinds it does not know by printing
//     "Warning: unhandled element ..." to stdout, which corrupts both piped
//     output and the pager's screen.
//   - footnote references are inline nodes, and glamour buffers block content
//     internally, so a node renderer writing straight to the output puts the
//     marker in the wrong place in the document.
//
// Definitions are removed from the source and the notes are rendered after the
// document, which is both simpler and more predictable than either workaround.
func extractFootnotes(src string) (body string, notes []footnote) {
	lines := strings.Split(src, "\n")
	keep := make([]bool, len(lines))
	defs := make(map[string]string)

	var fence string
	for i := 0; i < len(lines); i++ {
		line := lines[i]

		if marker := fenceMarker(line); marker != "" {
			if fence == "" {
				fence = marker
			} else if strings.HasPrefix(marker, fence) {
				fence = ""
			}
			keep[i] = true
			continue
		}
		if fence != "" {
			keep[i] = true
			continue
		}

		match := footnoteDef.FindStringSubmatch(line)
		if match == nil {
			keep[i] = true
			continue
		}

		// Consume the definition and any indented continuation lines.
		parts := []string{strings.TrimSpace(match[2])}
		j := i + 1
		for j < len(lines) {
			next := lines[j]
			if strings.TrimSpace(next) == "" {
				if j+1 < len(lines) && isIndented(lines[j+1]) {
					parts = append(parts, "")
					j++
					continue
				}
				break
			}
			if !isIndented(next) {
				break
			}
			parts = append(parts, strings.TrimSpace(next))
			j++
		}
		text := strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
		if _, exists := defs[match[1]]; !exists {
			defs[match[1]] = text
		}
		for k := i; k < j; k++ {
			keep[k] = false
		}
		i = j - 1
	}

	kept := make([]string, 0, len(lines))
	for i, line := range lines {
		if keep[i] {
			kept = append(kept, line)
		}
	}

	// Number the references in the order a reader meets them, and rewrite them
	// as plain text so that glamour renders them in place.
	numbering := make(map[string]int)
	body = strings.Join(replaceFootnotes(kept, defs, numbering), "\n")
	for label, n := range numbering {
		notes = append(notes, footnote{number: n, label: label, text: defs[label]})
	}
	sortNotes(notes)
	return body, notes
}

// replaceFootnotes rewrites references in the kept lines, assigning numbers in
// order of first reference. References inside code are left alone.
func replaceFootnotes(lines []string, defs map[string]string, numbering map[string]int) []string {
	out := make([]string, 0, len(lines))
	var fence string
	next := 1

	for _, line := range lines {
		if marker := fenceMarker(line); marker != "" {
			if fence == "" {
				fence = marker
			} else if strings.HasPrefix(marker, fence) {
				fence = ""
			}
			out = append(out, line)
			continue
		}
		if fence != "" {
			out = append(out, line)
			continue
		}

		// Only rewrite outside of `inline code` spans: splitting on the
		// backtick leaves the odd-indexed parts inside code.
		parts := strings.Split(line, "`")
		for i := 0; i < len(parts); i += 2 {
			parts[i] = footnoteRef.ReplaceAllStringFunc(parts[i], func(match string) string {
				label := match[2 : len(match)-1]
				if _, known := defs[label]; !known {
					return match // reference with no definition: leave it as written
				}
				if _, seen := numbering[label]; !seen {
					numbering[label] = next
					next++
				}
				return "[" + strconv.Itoa(numbering[label]) + "]"
			})
		}
		out = append(out, strings.Join(parts, "`"))
	}
	return out
}

// sortNotes orders notes by their number.
func sortNotes(notes []footnote) {
	for i := 1; i < len(notes); i++ {
		for j := i; j > 0 && notes[j].number < notes[j-1].number; j-- {
			notes[j], notes[j-1] = notes[j-1], notes[j]
		}
	}
}

// footnotesSection renders the notes after the document.
func (r *Renderer) footnotesSection(notes []footnote) string {
	if len(notes) == 0 {
		return ""
	}
	p := r.caps.Palette
	pad := strings.Repeat(" ", docMargin)
	rule := lipgloss.NewStyle().Foreground(theme.Color(p.Rule()))
	head := lipgloss.NewStyle().Foreground(theme.Color(p.Heading())).Bold(true)
	marker := lipgloss.NewStyle().Foreground(theme.Color(p.Accent()))

	var b strings.Builder
	b.WriteString("\n" + pad + r.paint(rule, strings.Repeat(r.ruleGlyph(), r.ruleWidth())) + "\n")
	b.WriteString(pad + r.paint(head, "Footnotes") + "\n\n")

	for _, note := range notes {
		mark := strconv.Itoa(note.number) + "."
		width := max(r.width-docMargin-levelIndent-len(mark)-1, minPanelWidth)
		for i, line := range strings.Split(lipgloss.Wrap(note.text, width, ""), "\n") {
			lead := pad + strings.Repeat(" ", levelIndent) + r.paint(marker, mark) + " "
			if i > 0 {
				lead = pad + strings.Repeat(" ", levelIndent+len(mark)+1)
			}
			b.WriteString(lead + line + "\n")
		}
	}
	return b.String()
}

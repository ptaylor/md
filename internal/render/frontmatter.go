package render

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/pftylr/md/internal/theme"
)

// splitFrontMatter separates a leading YAML ("---") or TOML ("+++") front
// matter block from the document body.
//
// The block is removed from the source rather than left in place: goldmark has
// no notion of front matter and would render the fences as a thematic break
// followed by a stray paragraph. The body is otherwise untouched, so the rest of
// the document parses exactly as written.
func splitFrontMatter(src string) (body, front string) {
	src = strings.TrimPrefix(src, "\ufeff")
	lines := strings.Split(src, "\n")
	if len(lines) == 0 {
		return src, ""
	}
	fence := strings.TrimSpace(strings.TrimSuffix(lines[0], "\r"))
	if fence != "---" && fence != "+++" {
		return src, ""
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(strings.TrimSuffix(lines[i], "\r")) == fence {
			return strings.Join(lines[i+1:], "\n"), strings.Join(lines[1:i], "\n")
		}
	}
	return src, "" // no closing fence, so it was never front matter
}

// frontMatterPanel renders front matter as a metadata panel above the document.
func (r *Renderer) frontMatterPanel(front string, width int) string {
	front = strings.TrimSpace(front)
	if front == "" {
		return ""
	}
	lines := r.frontMatterLines(front)
	panel := r.panel("metadata", lines, docMargin, width)
	return strings.Join(panel, "\n") + "\n"
}

// frontMatterLines lays out the metadata as aligned key/value rows, falling
// back to the raw text when the block is nested or otherwise too complex to
// flatten.
func (r *Renderer) frontMatterLines(front string) []string {
	p := r.caps.Palette
	key := lipgloss.NewStyle().Foreground(theme.Color(p.Dim()))
	val := lipgloss.NewStyle().Foreground(theme.Color(p.Foreground()))

	rows, ok := frontMatterRows(front)
	if !ok {
		return r.dimLines(splitLines(front))
	}
	width := 0
	for _, row := range rows {
		width = max(width, len(row.key))
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		switch {
		case row.key == "":
			out = append(out, r.paint(key, row.value))
		case row.value == "":
			out = append(out, r.paint(key, row.key))
		default:
			pad := strings.Repeat(" ", width-len(row.key))
			out = append(out, r.paint(key, row.key)+pad+"  "+r.paint(val, row.value))
		}
	}
	return out
}

type fmRow struct{ key, value string }

// frontMatterRows flattens simple "key: value" and "key = value" pairs. It
// reports false for anything nested, which is rendered verbatim instead.
func frontMatterRows(front string) ([]fmRow, bool) {
	var rows []fmRow
	for _, line := range strings.Split(front, "\n") {
		line = strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if line != trimmed {
			return nil, false // indented, so part of a nested structure
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			rows = append(rows, fmRow{value: strings.Trim(trimmed, "[]")})
			continue
		}
		key, value, ok := cutKeyValue(trimmed)
		if !ok {
			return nil, false
		}
		rows = append(rows, fmRow{key: key, value: cleanValue(value)})
	}
	return rows, len(rows) > 0
}

// cutKeyValue splits a front matter line on the first YAML or TOML separator.
func cutKeyValue(line string) (key, value string, ok bool) {
	for _, sep := range []string{":", "="} {
		if i := strings.Index(line, sep); i > 0 {
			return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+len(sep):]), true
		}
	}
	return "", "", false
}

// cleanValue tidies a scalar value for display: quotes are dropped and inline
// lists are joined with commas.
func cleanValue(v string) string {
	if len(v) >= 2 {
		switch {
		case strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]"):
			parts := strings.Split(strings.Trim(v, "[]"), ",")
			for i := range parts {
				parts[i] = strings.Trim(strings.TrimSpace(parts[i]), `"'`)
			}
			return strings.Join(parts, ", ")
		case (strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`)) ||
			(strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'")):
			return v[1 : len(v)-1]
		}
	}
	return v
}

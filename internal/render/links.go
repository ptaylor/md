package render

import (
	"regexp"
	"strings"
)

// OSC 8 hyperlink sequences are emitted by glamour around both the link text
// and the URL that follows it. The two runs are always adjacent, which is what
// lets md drop the URL on terminals where the link text itself is clickable.
var (
	// hyperlinkPair matches "text run + space + URL run" as a single unit.
	hyperlinkPair = regexp.MustCompile(
		`(\x1b\]8;[^\x1b\x07]*(?:\x07|\x1b\\))([^\x1b]*)(\x1b\]8;;(?:\x07|\x1b\\))` +
			` ` +
			`(\x1b\]8;[^\x1b\x07]*(?:\x07|\x1b\\))([^\x1b]*)(\x1b\]8;;(?:\x07|\x1b\\))`)

	// hyperlinkAny matches any OSC 8 sequence on its own.
	hyperlinkAny = regexp.MustCompile(`\x1b\]8;[^\x1b\x07]*(?:\x07|\x1b\\)`)
)

// applyLinkMode post-processes rendered links.
//
// glamour always prints the URL after the link text, wrapped in an OSC 8
// hyperlink. That is the right thing to do on terminals without OSC 8 support,
// where the escape sequences are ignored and the URL would otherwise be lost,
// but it is noise where the link text is clickable. The mode is chosen from the
// terminal's capabilities unless the user overrides it.
func (r *Renderer) applyLinkMode(out string) string {
	// Colour is off, which means the OSC 8 escapes are stripped at the end of
	// the render. Hiding the URL would then leave no way to see where a link
	// points, so the URL always stays in this mode.
	if r.mono() {
		return out
	}

	mode := strings.ToLower(strings.TrimSpace(r.opts.Links))
	if mode == "" {
		mode = "auto"
	}
	if mode == "auto" {
		if !r.caps.OSC8 {
			return out
		}
		mode = "inline"
	}
	switch mode {
	case "inline":
		return hyperlinkPair.ReplaceAllString(out, "${1}${2}${3}")
	case "plain":
		return hyperlinkAny.ReplaceAllString(out, "")
	default: // "both"
		return out
	}
}

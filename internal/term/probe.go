package term

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/pftylr/md/internal/cache"
	"time"

	xansi "github.com/charmbracelet/x/ansi"
	xterm "github.com/charmbracelet/x/term"

	"github.com/pftylr/md/internal/theme"
)

const (
	// probeTimeout bounds the whole query round trip. A terminal that does not
	// answer costs md this delay once, after which the result is cached.
	probeTimeout = 90 * time.Millisecond
	// paletteTTL is how long a cached probe result is reused.
	paletteTTL = time.Hour
)

// oscResponse matches a terminal reply to an OSC query, for example
// "\x1b]4;1;rgb:cd00/0000/0000\x1b\\" or "\x1b]11;rgb:1e1e/1e1e/1e1e\a".
var oscResponse = regexp.MustCompile(`\x1b\]([0-9]+);([^\x07\x1b]*)(?:\x07|\x1b\\)`)

// ProbeResult records what a terminal disclosed about its own colours.
type ProbeResult struct {
	// Answered is false when the terminal did not reply at all, which is the
	// case for Apple Terminal and other terminals without OSC 4 support.
	Answered bool
	Slots    [theme.NumSlots]color.RGBA
	Covered  int
	FG, BG   color.RGBA
	HasFG    bool
	HasBG    bool
	Dark     bool
}

// Complete reports whether every ANSI slot was reported. Only then is it safe
// to bypass the terminal's palette and emit RGB values directly.
func (r ProbeResult) Complete() bool { return r.Covered == theme.NumSlots }

// Palette turns a probe result into a rendering palette. Terminals that
// answered partially keep referring to ANSI slots so the terminal still paints
// its own colours.
func (r ProbeResult) Palette(dark bool) theme.Palette {
	if !r.Answered {
		return theme.Default(dark)
	}
	if !r.Complete() {
		return theme.Default(dark)
	}
	fg, bg := r.FG, r.BG
	if !r.HasFG {
		fg = theme.Default(dark).FG
	}
	if !r.HasBG {
		bg = theme.Default(dark).BG
	}
	return theme.Probed(r.Slots, fg, bg, dark)
}

// QueryPalette asks the terminal for its foreground, background and 16 ANSI
// colours, consulting the on-disk cache first. It never blocks for longer than
// probeTimeout.
func QueryPalette(tty *os.File, refresh bool) ProbeResult {
	if tty == nil {
		return ProbeResult{}
	}
	key := cacheKey()
	if !refresh {
		if r, ok := loadCached(key); ok {
			return r
		}
	}
	r := probe(tty)
	saveCached(key, r)
	return r
}

// probe performs the OSC 4/10/11 query round trip.
func probe(tty *os.File) ProbeResult {
	fd := tty.Fd()
	state, err := xterm.MakeRaw(fd)
	if err != nil {
		return ProbeResult{}
	}
	defer func() { _ = xterm.Restore(fd, state) }()

	var q bytes.Buffer
	q.WriteString("\x1b]10;?\x07") // default foreground
	q.WriteString("\x1b]11;?\x07") // default background
	for i := range theme.NumSlots {
		fmt.Fprintf(&q, "\x1b]4;%d;?\x07", i)
	}
	if _, err := tty.Write(q.Bytes()); err != nil {
		return ProbeResult{}
	}
	return parseProbe(readReply(tty, probeTimeout))
}

// readReply reads terminal responses until everything we asked for has arrived
// or the timeout expires. Deadlines are required: without them a terminal that
// answers nothing would block startup forever, and a background reader would
// steal keystrokes from the pager.
func readReply(tty *os.File, timeout time.Duration) []byte {
	deadline := time.Now().Add(timeout)
	if err := tty.SetReadDeadline(deadline); err != nil {
		// Character devices without deadline support would block forever.
		return nil
	}
	var buf bytes.Buffer
	chunk := make([]byte, 4096)
	for time.Now().Before(deadline) {
		n, err := tty.Read(chunk)
		if n > 0 {
			buf.Write(chunk[:n])
			if complete(buf.Bytes()) {
				break
			}
		}
		if err != nil {
			break // deadline exceeded or EOF
		}
	}
	return buf.Bytes()
}

// complete reports whether we have seen every reply we expect.
func complete(b []byte) bool {
	fg, bg, slots := false, false, 0
	for _, m := range oscResponse.FindAllSubmatch(b, -1) {
		switch string(m[1]) {
		case "10":
			fg = true
		case "11":
			bg = true
		case "4":
			slots++
		}
	}
	return fg && bg && slots >= theme.NumSlots
}

// parseProbe extracts colours from accumulated terminal responses.
func parseProbe(b []byte) ProbeResult {
	var r ProbeResult
	if len(b) == 0 {
		return r
	}
	var have [theme.NumSlots]bool
	for _, m := range oscResponse.FindAllSubmatch(b, -1) {
		payload := string(m[2])
		switch string(m[1]) {
		case "10", "11":
			c, ok := parseColor(payload)
			if !ok {
				continue
			}
			if string(m[1]) == "10" {
				r.FG, r.HasFG = c, true
			} else {
				r.BG, r.HasBG = c, true
			}
		case "4":
			idx, spec, found := strings.Cut(payload, ";")
			if !found {
				continue
			}
			i, err := strconv.Atoi(strings.TrimSpace(idx))
			if err != nil || i < 0 || i >= theme.NumSlots {
				continue
			}
			c, ok := parseColor(spec)
			if !ok {
				continue
			}
			r.Slots[i], have[i] = c, true
		}
	}
	for _, ok := range have {
		if ok {
			r.Covered++
		}
	}
	if r.Covered == 0 && !r.HasBG {
		return r
	}
	// Slots the terminal did not report fall back to the xterm defaults.
	for i, ok := range have {
		if !ok {
			r.Slots[i] = theme.DefaultSlot(i)
		}
	}
	r.Answered = true
	if !r.HasBG {
		r.BG = theme.Default(true).BG
	}
	r.Dark = theme.Luminance(r.BG) < 0.5
	return r
}

// Colour specifications a terminal may reply with.
var (
	namedHex = regexp.MustCompile(`^#[0-9a-fA-F]{3}([0-9a-fA-F]{3})?$`)
	rgbSpec  = regexp.MustCompile(`^rgba?:[0-9a-fA-F]{1,4}(/[0-9a-fA-F]{1,4}){2,3}$`)
)

// parseColor understands the "rgb:rrrr/gggg/bbbb" form terminals reply with,
// plus the "#rrggbb" form some emulators use.
//
// The specification is validated here rather than left to the parser: xansi
// ignores unparseable hex in an rgb: reply and silently returns black, so a
// corrupted or unsolicited reply would otherwise be adopted as a real colour.
func parseColor(spec string) (color.RGBA, bool) {
	spec = strings.TrimSpace(spec)
	if !namedHex.MatchString(spec) && !rgbSpec.MatchString(spec) {
		return color.RGBA{}, false
	}
	c := xansi.XParseColor(spec)
	if c == nil {
		return color.RGBA{}, false
	}
	r, g, b, _ := c.RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 0xff}, true //nolint:gosec
}

// cacheKey identifies the terminal settings a cached palette belongs to, so
// that switching emulators or TERM does not reuse a stale result.
func cacheKey() string {
	parts := []string{
		os.Getenv("TERM"),
		os.Getenv("TERM_PROGRAM"),
		os.Getenv("TERM_PROGRAM_VERSION"),
		os.Getenv("COLORTERM"),
		os.Getenv("COLORFGBG"),
	}
	return strings.Join(parts, "|")
}

type cachedPalette struct {
	At      int64    `json:"at"`
	Key     string   `json:"key"`
	Present bool     `json:"present"`
	Slots   []string `json:"slots,omitempty"`
	FG      string   `json:"fg,omitempty"`
	BG      string   `json:"bg,omitempty"`
	Dark    bool     `json:"dark"`
}

func cachePath() string {
	return cache.PalettePath()
}

func loadCached(key string) (ProbeResult, bool) {
	path := cachePath()
	if path == "" {
		return ProbeResult{}, false
	}
	raw, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return ProbeResult{}, false
	}
	var c cachedPalette
	if err := json.Unmarshal(raw, &c); err != nil {
		return ProbeResult{}, false
	}
	if c.Key != key || time.Since(time.Unix(c.At, 0)) > paletteTTL {
		return ProbeResult{}, false
	}
	if !c.Present {
		// Remembered "terminal did not answer" result: report a cache hit so
		// the next run does not pay the probe timeout again.
		return ProbeResult{}, true
	}
	r := ProbeResult{Answered: true, HasBG: true, Dark: c.Dark}
	for i, s := range c.Slots {
		if i >= theme.NumSlots {
			break
		}
		if col, ok := parseColor(s); ok {
			r.Slots[i] = col
			r.Covered++
		}
	}
	if col, ok := parseColor(c.BG); ok {
		r.BG = col
	}
	if col, ok := parseColor(c.FG); ok {
		r.FG, r.HasFG = col, true
	}
	if r.Covered == 0 {
		return ProbeResult{}, false
	}
	return r, true
}

func saveCached(key string, r ProbeResult) {
	path := cachePath()
	if path == "" {
		return
	}
	c := cachedPalette{At: time.Now().Unix(), Key: key, Present: r.Answered, Dark: r.Dark}
	if r.Answered {
		c.Slots = make([]string, 0, theme.NumSlots)
		for i := range theme.NumSlots {
			c.Slots = append(c.Slots, theme.Hex(r.Slots[i]))
		}
		c.FG, c.BG = theme.Hex(r.FG), theme.Hex(r.BG)
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return
	}
	_ = os.WriteFile(path, raw, 0o600)
}

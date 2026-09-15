package term

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/pftylr/md/internal/theme"
)

var hexPattern = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// fullReply builds the reply a terminal like iTerm2 sends for a complete probe:
// default foreground and background plus all 16 palette slots, mixing the two
// terminators terminals use (ESC \\ and BEL).
func fullReply() []byte {
	var b strings.Builder
	b.WriteString("\x1b]10;rgb:e5e5/e5e5/e5e5\x1b\\")
	b.WriteString("\x1b]11;rgb:1e1e/1e1e/1e1e\x07")
	for i := range theme.NumSlots {
		fmt.Fprintf(&b, "\x1b]4;%d;rgb:%02x00/%02x00/%02x00\x1b\\", i, i+1, i+2, i+3)
	}
	return []byte(b.String())
}

func TestParseProbeComplete(t *testing.T) {
	r := parseProbe(fullReply())

	if !r.Answered {
		t.Fatal("a full reply should count as answered")
	}
	if !r.Complete() {
		t.Errorf("expected all %d slots, got %d", theme.NumSlots, r.Covered)
	}
	if !r.Dark {
		t.Error("a background of #1e1e1e should be detected as dark")
	}
	if got := theme.Hex(r.Slots[0]); got != "#010203" {
		t.Errorf("slot 0 = %s, want #010203", got)
	}
	if got := theme.Hex(r.Slots[15]); got != "#101112" {
		t.Errorf("slot 15 = %s, want #101112", got)
	}
	if got := theme.Hex(r.BG); got != "#1e1e1e" {
		t.Errorf("background = %s, want #1e1e1e", got)
	}
	if got := theme.Hex(r.FG); got != "#e5e5e5" {
		t.Errorf("foreground = %s, want #e5e5e5", got)
	}
}

func TestParseProbeLightBackground(t *testing.T) {
	reply := strings.Replace(string(fullReply()), "rgb:1e1e/1e1e/1e1e", "rgb:ffff/ffff/ffff", 1)
	r := parseProbe([]byte(reply))
	if r.Dark {
		t.Error("a white background should be detected as light")
	}
}

// TestParseProbePartial is the case that matters for correctness: a terminal
// that answers the background query but not the palette must not have RGB values
// invented for the slots it stayed silent about.
func TestParseProbePartial(t *testing.T) {
	reply := "\x1b]11;rgb:1e1e/1e1e/1e1e\x1b\\\x1b]4;0;rgb:0000/0000/0000\x1b\\"
	r := parseProbe([]byte(reply))

	if !r.Answered {
		t.Fatal("a partial reply still counts as an answer")
	}
	if r.Complete() {
		t.Error("a partial reply must not count as complete")
	}
	if r.Covered != 1 {
		t.Errorf("Covered = %d, want 1", r.Covered)
	}

	// An incomplete probe must fall back to colour indices so the terminal
	// paints the slots it never told us about.
	p := r.Palette(true)
	if p.Tier != theme.TierANSI {
		t.Errorf("palette tier = %v, want TierANSI", p.Tier)
	}
	if got := p.Slot(theme.Red); got == nil || *got != "1" {
		t.Errorf("slot for red = %v, want the ANSI index \"1\"", got)
	}
}

func TestParseProbeNoReply(t *testing.T) {
	r := parseProbe(nil)
	if r.Answered {
		t.Error("an empty reply must not count as answered")
	}
	if r.Complete() {
		t.Error("an empty reply must not count as complete")
	}
	if p := r.Palette(true); p.Tier != theme.TierANSI {
		t.Errorf("tier = %v, want the ANSI fallback", p.Tier)
	}
}

func TestParseProbeIgnoresRubbish(t *testing.T) {
	// Keystrokes typed while the probe was running, plus a malformed colour.
	reply := "hello\x1b]4;99;rgb:0000/0000/0000\x1b\\\x1b]4;2;nonsense\x1b\\\x1b]11;rgb:zz/zz/zz\x1b\\"
	r := parseProbe([]byte(reply))
	if r.Covered != 0 {
		t.Errorf("Covered = %d, want 0: out-of-range and malformed slots must be ignored", r.Covered)
	}
	if r.Answered {
		t.Error("no valid response should leave the probe unanswered")
	}
}

func TestParseColor(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{in: "rgb:cd00/0000/0000", want: "#cd0000", ok: true},
		{in: "rgb:ffff/ffff/ffff", want: "#ffffff", ok: true},
		{in: "#123456", want: "#123456", ok: true},
		// X11 allows one to four hex digits per component.
		{in: "rgb:12/34/56", ok: true},
		// Malformed replies must be rejected rather than adopted as black.
		{in: "rgb:zz/zz/zz", ok: false},
		{in: "rgb:1/2", ok: false},
		{in: "rgb:12345/1/1", ok: false},
		{in: "", ok: false},
		{in: "red", ok: false},
		{in: "\x1b]11;rgb:1e1e/1e1e/1e1e", ok: false},
	}
	for _, tc := range cases {
		got, ok := parseColor(tc.in)
		if ok != tc.ok {
			t.Errorf("parseColor(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		if tc.want != "" && theme.Hex(got) != tc.want {
			t.Errorf("parseColor(%q) = %s, want %s", tc.in, theme.Hex(got), tc.want)
		}
		if !hexPattern.MatchString(theme.Hex(got)) {
			t.Errorf("parseColor(%q) = %s, want an rgb hex value", tc.in, theme.Hex(got))
		}
	}
}

func TestCompleteRequiresEverything(t *testing.T) {
	full := fullReply()
	if !complete(full) {
		t.Error("a full reply should be recognised as complete")
	}
	// Drop the background reply: the probe should keep waiting for it.
	partial := []byte(strings.Replace(string(full), "\x1b]11;rgb:1e1e/1e1e/1e1e\x07", "", 1))
	if complete(partial) {
		t.Error("a reply missing the background should not be complete")
	}
}

// TestResolveDarkPrecedence pins the order of the background decision: an
// explicit theme wins, then the probed background, then the terminal's answer to
// an OSC 11 query.
func TestResolveDarkPrecedence(t *testing.T) {
	probedLight := ProbeResult{Answered: true, HasBG: true, Dark: false}
	probedDark := ProbeResult{Answered: true, HasBG: true, Dark: true}

	if resolveDark("dark", probedLight, nil) != true {
		t.Error("--theme dark must override a light probe")
	}
	if resolveDark("light", probedDark, nil) != false {
		t.Error("--theme light must override a dark probe")
	}
	if resolveDark("auto", probedLight, nil) != false {
		t.Error("an auto theme should follow the probed background")
	}
	if resolveDark("auto", probedDark, nil) != true {
		t.Error("an auto theme should follow the probed background")
	}
}

// unsetEnv removes a variable for the duration of the test, restoring its
// previous value afterwards. An empty value is not the same as an absent one:
// md treats MD_HYPERLINKS being present at all as an instruction.
func unsetEnv(t *testing.T, name string) {
	t.Helper()
	if v, ok := os.LookupEnv(name); ok {
		t.Setenv(name, v)
	}
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
}

func TestOSC8Detection(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{name: "iTerm2", env: map[string]string{"TERM_PROGRAM": "iTerm.app"}, want: true},
		{name: "Apple Terminal", env: map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, want: false},
		{name: "kitty", env: map[string]string{"TERM": "xterm-kitty"}, want: true},
		{name: "wezterm pane", env: map[string]string{"WEZTERM_PANE": "1"}, want: true},
		{name: "unknown", env: map[string]string{"TERM": "vt100"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range []string{"TERM_PROGRAM", "TERM", "WEZTERM_PANE", "KITTY_WINDOW_ID", "WT_SESSION", "VTE_VERSION", "MD_HYPERLINKS"} {
				unsetEnv(t, name)
			}
			for name, value := range tc.env {
				t.Setenv(name, value)
			}
			if got := osc8Supported(); got != tc.want {
				t.Errorf("osc8Supported() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOSC8Override(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	if osc8Supported() {
		t.Fatal("Apple Terminal should not claim OSC 8 support")
	}
	t.Setenv("MD_HYPERLINKS", "1")
	if !osc8Supported() {
		t.Error("MD_HYPERLINKS=1 should force hyperlinks on")
	}
}

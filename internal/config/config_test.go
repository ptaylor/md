package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearEnv removes every MD_* variable so tests see only what they set.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "MD_") {
			t.Setenv(name, "")
			_ = os.Unsetenv(name)
		}
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("MD_CONFIG", filepath.Join(t.TempDir(), "absent.toml"))

	c, err := Load()
	if err != nil {
		t.Fatalf("a missing config file must not be an error: %v", err)
	}
	if c.Theme != "auto" || c.Pager != "auto" || c.Mermaid != "auto" || c.Links != "auto" {
		t.Errorf("unexpected defaults: %+v", c)
	}
	if c.MaxWidth != 100 {
		t.Errorf("MaxWidth = %d, want 100", c.MaxWidth)
	}
}

func TestFileSettings(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, `
# A comment.
width = 90
max_width = 72
theme = "light"
pager = "none"
mermaid = "off"
links = "plain"
ascii = true
no_color = yes
no_probe = 1
`)
	t.Setenv("MD_CONFIG", path)

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		Width: 90, MaxWidth: 72,
		Theme: "light", Pager: "none", Mermaid: "off", Links: "plain",
		Ascii: true, NoColor: true, NoProbe: true,
	}
	if c != want {
		t.Errorf("got  %+v\nwant %+v", c, want)
	}
}

// TestEnvironmentOverridesFile pins the documented precedence: flags beat the
// environment, and the environment beats the file.
func TestEnvironmentOverridesFile(t *testing.T) {
	clearEnv(t)
	t.Setenv("MD_CONFIG", writeConfig(t, "theme = \"dark\"\nmax_width = 80\n"))
	t.Setenv("MD_THEME", "light")
	t.Setenv("MD_MAX_WIDTH", "120")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Theme != "light" {
		t.Errorf("Theme = %q, want the environment to win with \"light\"", c.Theme)
	}
	if c.MaxWidth != 120 {
		t.Errorf("MaxWidth = %d, want the environment to win with 120", c.MaxWidth)
	}
}

func TestInvalidSettingsAreReported(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "enum", body: "theme = \"purple\"\n", want: "expected one of auto, dark, light, mono"},
		{name: "number", body: "width = wide\n", want: "expected a non-negative number"},
		{name: "negative", body: "width = -5\n", want: "expected a non-negative number"},
		{name: "bool", body: "ascii = maybe\n", want: "expected true or false"},
		{name: "unknown key", body: "colour = \"red\"\n", want: "unknown setting"},
		{name: "not a pair", body: "theme\n", want: "expected key = value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("MD_CONFIG", writeConfig(t, tc.body))
			_, err := Load()
			if err == nil {
				t.Fatalf("expected an error for %q", tc.body)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
			// The message should point at the offending line.
			if !strings.Contains(err.Error(), ":1:") {
				t.Errorf("error %q does not report the line number", err)
			}
		})
	}
}

func TestPath(t *testing.T) {
	clearEnv(t)
	t.Setenv("MD_CONFIG", "/tmp/explicit.toml")
	if got := Path(); got != "/tmp/explicit.toml" {
		t.Errorf("Path() = %q, want $MD_CONFIG", got)
	}

	_ = os.Unsetenv("MD_CONFIG")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	if got := Path(); got != "/tmp/xdg/md/config.toml" {
		t.Errorf("Path() = %q, want it to honour $XDG_CONFIG_HOME", got)
	}
}

// TestEnvNamesDoNotCollide guards against MD_PAGER (the pager command) and the
// pager mode being confused for one another.
func TestEnvNamesDoNotCollide(t *testing.T) {
	clearEnv(t)
	t.Setenv("MD_CONFIG", filepath.Join(t.TempDir(), "absent.toml"))
	t.Setenv("MD_PAGER", "less -R")
	t.Setenv("MD_PAGER_MODE", "none")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Pager != "none" {
		t.Errorf("Pager = %q, want %q from MD_PAGER_MODE", c.Pager, "none")
	}
}

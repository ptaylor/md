// Package config reads md's settings, from a config file and the environment.
//
// Precedence is: command line flags, then environment variables, then
// ~/.config/md/config.toml, then built-in defaults. The file format is a flat
// TOML subset - "key = value", with # comments - so md needs no TOML parser
// dependency for the handful of settings it has.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds md's settings.
type Config struct {
	// Width is the render width in columns; 0 means the terminal width.
	Width int
	// MaxWidth caps the content width so long lines stay readable.
	MaxWidth int
	// Theme is "auto", "dark", "light" or "mono".
	Theme string
	// Pager is "auto", "builtin", "less" or "none".
	Pager string
	// Mermaid is "auto", "box" or "off".
	Mermaid string
	// MermaidCmd overrides the mmdc executable to run.
	MermaidCmd string
	// Links is "auto", "inline", "both" or "plain".
	Links string
	// Ascii forces ASCII glyphs instead of box drawing characters.
	Ascii bool
	// NoColor disables colour.
	NoColor bool
	// NoProbe skips querying the terminal for its palette.
	NoProbe bool
}

// Defaults returns md's built-in settings.
func Defaults() Config {
	return Config{
		MaxWidth: 100,
		Theme:    "auto",
		Pager:    "auto",
		Mermaid:  "auto",
		Links:    "auto",
	}
}

// Load builds the effective configuration.
func Load() (Config, error) {
	c := Defaults()
	if err := applyFile(&c, Path()); err != nil {
		return c, err
	}
	applyEnv(&c)
	return c, nil
}

// Path returns the config file md reads, honouring $MD_CONFIG and
// $XDG_CONFIG_HOME.
func Path() string {
	if p := os.Getenv("MD_CONFIG"); p != "" {
		return p
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "md", "config.toml")
}

// applyFile reads a flat TOML subset. A missing file is not an error, so md
// works with no configuration at all.
func applyFile(c *Config, path string) error {
	if path == "" {
		return nil
	}
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") || strings.HasPrefix(text, "[") {
			continue
		}
		key, value, found := strings.Cut(text, "=")
		if !found {
			return fmt.Errorf("%s:%d: expected key = value", path, line)
		}
		if err := c.set(strings.TrimSpace(key), unquote(strings.TrimSpace(value))); err != nil {
			return fmt.Errorf("%s:%d: %w", path, line, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	return nil
}

// applyEnv lets MD_* variables override the file.
func applyEnv(c *Config) {
	if v, ok := os.LookupEnv("MD_WIDTH"); ok {
		_ = c.set("width", v)
	}
	if v, ok := os.LookupEnv("MD_MAX_WIDTH"); ok {
		_ = c.set("max_width", v)
	}
	for key, env := range map[string]string{
		"theme":       "MD_THEME",
		"pager":       "MD_PAGER_MODE",
		"mermaid":     "MD_MERMAID",
		"mermaid_cmd": "MD_MERMAID_CMD",
		"links":       "MD_LINKS",
	} {
		if v, ok := os.LookupEnv(env); ok {
			_ = c.set(key, v)
		}
	}
	for key, env := range map[string]string{
		"ascii":    "MD_ASCII",
		"no_color": "MD_NO_COLOR",
		"no_probe": "MD_NO_PROBE",
	} {
		if v, ok := os.LookupEnv(env); ok {
			_ = c.set(key, v)
		}
	}
}

// set assigns one setting, validating enumerations and numbers so that a typo
// is reported rather than silently ignored.
func (c *Config) set(key, value string) error {
	switch key {
	case "width":
		return setInt(&c.Width, value)
	case "max_width":
		return setInt(&c.MaxWidth, value)
	case "theme":
		return setEnum(&c.Theme, value, "auto", "dark", "light", "mono")
	case "pager":
		return setEnum(&c.Pager, value, "auto", "builtin", "less", "none")
	case "mermaid":
		return setEnum(&c.Mermaid, value, "auto", "box", "off")
	case "mermaid_cmd":
		c.MermaidCmd = unquote(value)
		return nil
	case "links":
		return setEnum(&c.Links, value, "auto", "inline", "both", "plain")
	case "ascii":
		return setBool(&c.Ascii, value)
	case "no_color":
		return setBool(&c.NoColor, value)
	case "no_probe":
		return setBool(&c.NoProbe, value)
	default:
		return fmt.Errorf("unknown setting %q", key)
	}
}

func setInt(dst *int, value string) error {
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return fmt.Errorf("expected a non-negative number, got %q", value)
	}
	*dst = n
	return nil
}

func setEnum(dst *string, value string, allowed ...string) error {
	lower := strings.ToLower(value)
	for _, a := range allowed {
		if lower == a {
			*dst = lower
			return nil
		}
	}
	return fmt.Errorf("expected one of %s, got %q", strings.Join(allowed, ", "), value)
}

func setBool(dst *bool, value string) error {
	switch strings.ToLower(value) {
	case "true", "yes", "on", "1":
		*dst = true
	case "false", "no", "off", "0":
		*dst = false
	default:
		return fmt.Errorf("expected true or false, got %q", value)
	}
	return nil
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

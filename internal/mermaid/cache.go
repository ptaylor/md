package mermaid

import (
	"os"
	"path/filepath"
)

// cacheDir returns where the diagram cache lives: alongside the palette probe's
// findings, under the user's cache directory.
func cacheDir(override string) string {
	if override != "" {
		return override
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "md", "diagrams")
}

// cachePath returns the file for a key, or "" when caching is impossible.
func cachePath(override, key string) string {
	dir := cacheDir(override)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, key+".svg")
}

// readCache returns a cached SVG if there is one.
func readCache(override, key string) ([]byte, bool) {
	path := cachePath(override, key)
	if path == "" {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return data, true
}

// writeCache stores an SVG for next time. Failing to cache is not worth
// reporting: the drawing has already been done.
func writeCache(override, key string, svg []byte) {
	path := cachePath(override, key)
	if path == "" {
		return
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return
	}
	// Write and rename, so a reader never sees half a diagram.
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return
	}
	name := tmp.Name()
	if _, err := tmp.Write(svg); err != nil {
		tmp.Close()     //nolint:errcheck,gosec // best effort
		os.Remove(name) //nolint:errcheck,gosec // best effort
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name) //nolint:errcheck,gosec // best effort
		return
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name) //nolint:errcheck,gosec // best effort
	}
}

package mermaid

import (
	"os"
	"path/filepath"

	"github.com/pftylr/md/internal/cache"
)

// cacheVersion is part of every cache key. Bump it when the meaning of a cached
// file changes, so that entries written by an older md are ignored rather than
// misread.
const cacheVersion = "md-mermaid-2"

// failureSuffix marks a cached refusal: the diagram mmdc would not draw, and what
// it said about it.
const failureSuffix = ".failed"

// cacheDir returns where the diagram cache lives: alongside the palette probe's
// findings, under the directory md keeps its caches in.
func cacheDir(override string) string {
	if override != "" {
		return override
	}
	return cache.DiagramsDir()
}

// cachePath returns the file for a key, or "" when caching is impossible.
func cachePath(override, key string) string {
	dir := cacheDir(override)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, key)
}

// readIfFresh returns a cached file, but only one written since the tool was last
// changed: a new mmdc redraws rather than serving a picture drawn by the old one.
func readIfFresh(path, toolPath string) ([]byte, bool) {
	if path == "" || !writtenSince(path, toolPath) {
		return nil, false
	}
	data, err := os.ReadFile(path) //nolint:gosec // md's own cache directory
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return data, true
}

// writtenSince reports whether path was written after other, which is how a cache
// entry is aged against the tool that produced it. A missing entry, or a missing
// tool, is not fresh.
func writtenSince(path, other string) bool {
	entry, err := os.Stat(path)
	if err != nil {
		return false
	}
	tool, err := os.Stat(other)
	if err != nil {
		return false
	}
	return entry.ModTime().After(tool.ModTime())
}

// writeFile stores a cached copy, atomically, so a reader never sees half of one.
// Failing to cache is not worth reporting: the work has been done either way.
func writeFile(path string, data []byte) {
	if path == "" {
		return
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
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

// removeFile forgets a cached entry.
func removeFile(path string) {
	if path != "" {
		os.Remove(path) //nolint:errcheck,gosec // best effort
	}
}

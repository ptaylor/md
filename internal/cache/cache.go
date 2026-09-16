// Package cache locates the files md keeps between runs, and clears them.
//
// Two things are cached: the colours the terminal reports, and the SVG mmdc
// produces for a diagram. They live side by side under one directory, so that a
// reader has one place to look and one flag to clear.
package cache

import (
	"os"
	"path/filepath"
)

// Dir returns md's cache directory, or "" when there is none to be had.
//
// $XDG_CACHE_HOME wins whenever it is set, on every platform: someone who keeps
// their dotfiles to the XDG convention expects that variable to be honoured, and
// Go's own os.UserCacheDir ignores it on macOS, where it answers
// $HOME/Library/Caches instead. Without the variable, the platform's own answer
// is used, which on macOS is the directory the system excludes from backups.
func Dir() string {
	if dir := os.Getenv("XDG_CACHE_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "md")
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "md")
}

// PalettePath is the file the palette probe caches its findings in.
func PalettePath() string { return under("palette.json") }

// DiagramsDir is the directory rendered diagrams are kept in.
func DiagramsDir() string { return under("diagrams") }

// under joins a name onto the cache directory, or returns "" when there is no
// cache directory to use.
func under(name string) string {
	dir := Dir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, name)
}

// Clear removes everything md has cached. It reports whether there was anything
// to remove.
func Clear() (bool, error) {
	dir := Dir()
	if dir == "" {
		return false, nil
	}
	if _, err := os.Lstat(dir); err != nil {
		return false, nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return false, err
	}
	return true, nil
}

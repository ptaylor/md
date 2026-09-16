package cache

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestDirFollowsXDG pins the rule: the variable wins wherever it is set, because
// someone who keeps their dotfiles to the XDG convention expects it to be
// honoured - including on macOS, where the standard library ignores it.
func TestDirFollowsXDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)

	if got, want := Dir(), filepath.Join(dir, "md"); got != want {
		t.Errorf("Dir() = %s, want %s", got, want)
	}
	if got, want := PalettePath(), filepath.Join(dir, "md", "palette.json"); got != want {
		t.Errorf("PalettePath() = %s, want %s", got, want)
	}
	if got, want := DiagramsDir(), filepath.Join(dir, "md", "diagrams"); got != want {
		t.Errorf("DiagramsDir() = %s, want %s", got, want)
	}
}

// TestDirIgnoresUnusableXDG covers the cases the specification rules out: an
// empty variable, and a relative one.
func TestDirIgnoresUnusableXDG(t *testing.T) {
	base, err := os.UserCacheDir()
	if err != nil {
		t.Skipf("no platform cache directory: %v", err)
	}
	for _, value := range []string{"", "relative/cache"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", value)
			if got, want := Dir(), filepath.Join(base, "md"); got != want {
				t.Errorf("Dir() = %s want the platform default %s", got, want)
			}
		})
	}
}

// TestClearRemovesEverything checks that one flag clears both caches, since that
// is the whole point of keeping them in one directory.
func TestClearRemovesEverything(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)

	if err := os.MkdirAll(DiagramsDir(), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		PalettePath(),
		filepath.Join(DiagramsDir(), "diagram.svg"),
	} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cleared, err := Clear()
	if err != nil {
		t.Fatalf("Clear() = %v", err)
	}
	if !cleared {
		t.Error("Clear() reported nothing to clear, but both caches existed")
	}
	if _, err := os.Lstat(Dir()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the cache directory survived: %v", err)
	}

	// Clearing an empty cache is not an error, it just has nothing to say.
	cleared, err = Clear()
	if err != nil || cleared {
		t.Errorf("second Clear() = %v, %v, want no error and nothing cleared", cleared, err)
	}
}

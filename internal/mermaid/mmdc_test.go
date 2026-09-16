package mermaid

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubMMDC puts a fake mmdc first on the PATH.
//
// The tests must not need the real one: it is optional, it starts a browser, and
// a build machine may not have it. A stub also makes the failure paths - which
// are the ones that matter, because they decide whether a reader sees a diagram
// or the source - testable at all.
func stubMMDC(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "mmdc")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec // a test fixture
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// workingStub writes a valid SVG and records each invocation, so a test can tell
// rendering from caching.
const workingStub = `#!/bin/sh
if [ "$1" = "--version" ]; then echo 9.9.9; exit 0; fi
echo run >> "$MMDC_CALLS"
out=
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    *) shift ;;
  esac
done
printf '<svg viewBox="0 0 100 100"></svg>' > "$out"
`

func newTool(t *testing.T) (*Tool, string) {
	t.Helper()
	calls := filepath.Join(t.TempDir(), "calls")
	t.Setenv("MMDC_CALLS", calls)
	return &Tool{CacheDir: t.TempDir()}, calls
}

func countCalls(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(data), "run")
}

// TestToolReportsEachDiagramAsItStartsAndEnds pins the protocol the CLI relies
// on to show progress.
func TestToolReportsEachDiagramAsItStartsAndEnds(t *testing.T) {
	stubMMDC(t, workingStub)
	tool, calls := newTool(t)
	var mu sync.Mutex
	var events []Progress
	tool.Progress = func(p Progress) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, p)
	}

	sources := []string{"graph TD\n  A --> B", "sequenceDiagram\n  A->>B: hi"}
	if got := tool.Diagrams(context.Background(), sources); len(got) != 2 {
		t.Fatalf("got %d drawings, want 2", len(got))
	}

	// Diagrams are drawn at the same time, so the events are not in document
	// order: gather them per diagram instead.
	starts := map[int]Progress{}
	ends := map[int]Progress{}
	for _, p := range events {
		if p.Started {
			starts[p.Index] = p
		} else {
			ends[p.Index] = p
		}
	}
	for i, want := range []string{"graph TD", "sequenceDiagram"} {
		index := i + 1
		start, started := starts[index]
		end, ended := ends[index]
		if !started || !ended || start.Label != want {
			t.Errorf("diagram %d: got start %+v and end %+v, want a start and an end labelled %q",
				index, start, end, want)
			continue
		}
		if start.Total != 2 || start.Took != 0 {
			t.Errorf("start event %d = %+v, want one of two with no duration yet", index, start)
		}
		if end.Err != nil {
			t.Errorf("end event %d = %+v, want no error", index, end)
		}
	}
	if got := countCalls(t, calls); got != 2 {
		t.Errorf("mmdc ran %d times, want 2", got)
	}
}

// TestToolReportsCacheHitsOnce checks that a diagram served from the cache is
// reported in a single event: reporting the instant round trip twice would only
// double the noise.
func TestToolReportsCacheHitsOnce(t *testing.T) {
	stubMMDC(t, workingStub)
	tool, calls := newTool(t)
	sources := []string{"graph TD\n  A --> B", "pie\n  \"a\": 1"}

	tool.Diagrams(context.Background(), sources)
	after := countCalls(t, calls)

	var mu sync.Mutex
	var events []Progress
	tool.Progress = func(p Progress) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, p)
	}
	tool.Diagrams(context.Background(), sources)

	if len(events) != 2 {
		t.Fatalf("got %d events for 2 cached diagrams, want one each: %+v", len(events), events)
	}
	for _, p := range events {
		if !p.Cached {
			t.Errorf("cached event = %+v, want Cached set", p)
		}
		if p.Started || p.Took != 0 {
			t.Errorf("cached event = %+v, want neither a start nor a duration", p)
		}
	}
	if got := countCalls(t, calls); got != after {
		t.Errorf("mmdc ran again for a cached diagram: %d calls, was %d", got, after)
	}
}

// TestToolReportsFailure is the fallback path: a diagram mmdc cannot draw has to
// come back empty, be recorded, and not be cached.
func TestToolReportsFailure(t *testing.T) {
	stubMMDC(t, `#!/bin/sh
if [ "$1" = "--version" ]; then echo 9.9.9; exit 0; fi
echo run >> "$MMDC_CALLS"
echo "UnknownDiagramError: no" >&2
exit 1
`)
	tool, _ := newTool(t)
	var last Progress
	tool.Progress = func(p Progress) { last = p }

	got := tool.Diagrams(context.Background(), []string{"not a diagram"})
	if len(got) != 1 || len(got[0]) != 0 {
		t.Fatalf("got %q, want one empty drawing", got)
	}
	if last.Err == nil {
		t.Error("the end event carries no error")
	}
	if !strings.Contains(last.Err.Error(), "UnknownDiagramError") {
		t.Errorf("error = %v, want mmdc's own words", last.Err)
	}
	if failures := tool.Failures(); len(failures) != 1 {
		t.Errorf("Failures() = %v, want one", failures)
	}
	if failures := tool.Failures(); len(failures) != 0 {
		t.Errorf("Failures() reported the same failure twice: %v", failures)
	}
}

// TestToolAvailableFollowsThePath checks the decision that turns auto mode on and
// off: whether mmdc can be run at all.
func TestToolAvailableFollowsThePath(t *testing.T) {
	stubMMDC(t, workingStub)
	if !(&Tool{}).Available() {
		t.Error("Available() = false with a stub on the PATH, want true")
	}
	t.Setenv("PATH", t.TempDir())
	if (&Tool{}).Available() {
		t.Error("Available() = true with an empty PATH, want false")
	}
}

// TestToolTimeoutIsReported checks that a hung mmdc fails rather than hanging md.
func TestToolTimeoutIsReported(t *testing.T) {
	stubMMDC(t, `#!/bin/sh
if [ "$1" = "--version" ]; then echo 9.9.9; exit 0; fi
sleep 5
`)
	tool, _ := newTool(t)
	tool.Timeout = 50 * time.Millisecond

	if _, err := tool.Diagram(context.Background(), "graph TD"); err == nil {
		t.Fatal("want a timeout error, got none")
	}
}

func TestLabelNamesTheDiagram(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"graph TD\n  A --> B", "graph TD"},
		{"%% a comment\n\ngraph LR\n  A --> B", "graph LR"},
		{"\n\n  pie title Charts\n  \"a\": 1", "pie title Charts"},
		{"", "diagram"},
	} {
		if got := label(tc.source); got != tc.want {
			t.Errorf("label(%q) = %q, want %q", tc.source, got, tc.want)
		}
	}
	long := "pie title " + strings.Repeat("x", 80)
	if got := label(long); len([]rune(got)) > 41 {
		t.Errorf("label(%q) = %q, want it shortened", long, got)
	}
}

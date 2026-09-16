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
// a build machine may not have it. A stub also makes the paths that matter -
// caching, refusals, timeouts - testable at all, and lets a test count how many
// processes md actually spends.
func stubMMDC(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "mmdc")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec // a test fixture
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return path
}

// workingStub writes a valid SVG and records each invocation. It fails if it is
// ever asked for its version: md works that out from timestamps instead, and
// asking costs a process spawn on every run.
const workingStub = `#!/bin/sh
if [ "$1" = "--version" ]; then echo "asked for the version" >&2; exit 3; fi
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

// failingStub refuses the diagram, as mmdc does for one it cannot parse.
const failingStub = `#!/bin/sh
if [ "$1" = "--version" ]; then echo "asked for the version" >&2; exit 3; fi
echo run >> "$MMDC_CALLS"
echo "UnknownDiagramError: no" >&2
exit 1
`

func newTool(t *testing.T) (*Tool, string) {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	t.Setenv("MMDC_CALLS", calls)
	// Nothing ever creates this, so a stub that waits for it waits until the
	// timeout kills it.
	t.Setenv("MMDC_RELEASE", filepath.Join(dir, "never"))
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

// collector gathers progress events safely, since diagrams are drawn at once.
type collector struct {
	mu     sync.Mutex
	events []Progress
}

func (c *collector) add(p Progress) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, p)
}

func (c *collector) all() []Progress {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Progress(nil), c.events...)
}

// TestToolSpendsNoProcessOnAWarmRun is the point of keying the cache on the
// diagram alone: with everything cached, md runs nothing at all. Asking mmdc for
// its version would cost a process spawn - four hundred milliseconds of browser
// runtime startup - on every run that contains a diagram.
func TestToolSpendsNoProcessOnAWarmRun(t *testing.T) {
	stubMMDC(t, workingStub)
	tool, calls := newTool(t)
	sources := []string{"graph TD\n  A --> B", "pie\n  \"a\": 1"}

	tool.Diagrams(context.Background(), sources)
	if got := countCalls(t, calls); got != 2 {
		t.Fatalf("first run made %d calls, want one render each", got)
	}
	if err := os.Truncate(calls, 0); err != nil {
		t.Fatal(err)
	}

	var seen collector
	tool.Progress = seen.add
	tool.Diagrams(context.Background(), sources)

	if got := countCalls(t, calls); got != 0 {
		t.Errorf("a warm run made %d calls to mmdc, want none", got)
	}
	for _, p := range seen.all() {
		if !p.Cached {
			t.Errorf("event = %+v, want everything served from the cache", p)
		}
	}
}

// TestToolRemembersARefusal checks that a diagram mmdc will not draw is not put
// to it again: finding that out costs a browser launch, and the answer does not
// change on its own.
func TestToolRemembersARefusal(t *testing.T) {
	stubMMDC(t, failingStub)
	tool, calls := newTool(t)
	sources := []string{"this is not a diagram"}

	tool.Diagrams(context.Background(), sources)
	if got := countCalls(t, calls); got != 1 {
		t.Fatalf("first run made %d calls, want 1", got)
	}
	if failures := tool.Failures(); len(failures) != 1 {
		t.Fatalf("Failures() = %v, want the refusal reported once", failures)
	}

	var seen collector
	tool.Progress = seen.add
	got := tool.Diagrams(context.Background(), sources)

	if countCalls(t, calls) != 1 {
		t.Error("the refusal was put to mmdc a second time")
	}
	if len(got) != 1 || len(got[0]) != 0 {
		t.Errorf("got %q, want one empty drawing", got)
	}
	events := seen.all()
	if len(events) != 1 {
		t.Fatalf("got %d events for a remembered refusal, want one: %+v", len(events), events)
	}
	if events[0].Err == nil || !events[0].Cached {
		t.Errorf("event = %+v, want a remembered failure", events[0])
	}
	if failures := tool.Failures(); len(failures) != 1 {
		t.Errorf("Failures() = %v, want the remembered refusal reported too", failures)
	}
}

// TestToolDoesNotRememberATimeout keeps refusals honest: a timeout says the
// machine was busy, not that the diagram is undrawable, so it is asked again.
func TestToolDoesNotRememberATimeout(t *testing.T) {
	// The stub records its call and then blocks, so its call is logged even when
	// the machine is busy and the timeout fires quickly.
	stubMMDC(t, `#!/bin/sh
if [ "$1" = "--version" ]; then exit 3; fi
echo run >> "$MMDC_CALLS"
while [ ! -f "$MMDC_RELEASE" ]; do sleep 0.05; done
`)
	tool, calls := newTool(t)
	tool.Timeout = 400 * time.Millisecond

	tool.Diagrams(context.Background(), []string{"graph TD\n  A --> B"})
	tool.Diagrams(context.Background(), []string{"graph TD\n  A --> B"})

	if got := countCalls(t, calls); got != 2 {
		t.Errorf("mmdc ran %d times, want 2: a timeout should not be remembered", got)
	}
}

// TestToolRedrawsWhenTheToolChanges covers the invalidation that replaced asking
// for the version: a newer mmdc means the cached pictures were drawn by an older
// one.
func TestToolRedrawsWhenTheToolChanges(t *testing.T) {
	stub := stubMMDC(t, workingStub)
	tool, calls := newTool(t)
	sources := []string{"graph TD\n  A --> B"}

	tool.Diagrams(context.Background(), sources)
	if got := countCalls(t, calls); got != 1 {
		t.Fatalf("first run made %d calls, want 1", got)
	}

	// A reinstalled mmdc: same path, newer than anything already cached.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(stub, future, future); err != nil {
		t.Fatal(err)
	}
	tool.Diagrams(context.Background(), sources)

	if got := countCalls(t, calls); got != 2 {
		t.Errorf("mmdc ran %d times, want 2: a newer tool should redraw", got)
	}
}

// TestToolReportsEachDiagramAsItStartsAndEnds pins the protocol the CLI relies on
// to show progress.
func TestToolReportsEachDiagramAsItStartsAndEnds(t *testing.T) {
	stubMMDC(t, workingStub)
	tool, calls := newTool(t)
	var seen collector
	tool.Progress = seen.add

	sources := []string{"graph TD\n  A --> B", "sequenceDiagram\n  A->>B: hi"}
	if got := tool.Diagrams(context.Background(), sources); len(got) != 2 {
		t.Fatalf("got %d drawings, want 2", len(got))
	}

	starts := map[int]Progress{}
	ends := map[int]Progress{}
	for _, p := range seen.all() {
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

	var seen collector
	tool.Progress = seen.add
	tool.Diagrams(context.Background(), sources)

	events := seen.all()
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

// TestToolWithoutAToolReportsIt checks that a missing mmdc comes back as an
// error rather than a silent empty drawing.
func TestToolWithoutAToolReportsIt(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	tool, _ := newTool(t)

	got := tool.Diagrams(context.Background(), []string{"graph TD"})
	if len(got) != 1 || len(got[0]) != 0 {
		t.Errorf("got %q, want one empty drawing", got)
	}
	if failures := tool.Failures(); len(failures) != 1 {
		t.Errorf("Failures() = %v, want the missing tool reported", failures)
	}

	var seen collector
	tool.Progress = seen.add
	tool.Diagrams(context.Background(), []string{"graph TD"})
	var ended bool
	for _, p := range seen.all() {
		if p.Started {
			continue
		}
		ended = true
		if p.Err == nil {
			t.Errorf("end event = %+v, want an error", p)
		}
	}
	if !ended {
		t.Error("no end event came back for a missing tool")
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

// TestCacheKeyIsTheDiagram checks that the key depends on the diagram and nothing
// else, which is what lets a warm run avoid running mmdc to work out a version.
func TestCacheKeyIsTheDiagram(t *testing.T) {
	tool := &Tool{}
	first := tool.key("graph TD\n  A --> B")
	if first != tool.key("graph TD\n  A --> B") {
		t.Error("the same diagram produced two keys")
	}
	if first == tool.key("graph TD\n  A --> C") || first == tool.key("graph TD") {
		t.Error("different diagrams produced the same key")
	}
}

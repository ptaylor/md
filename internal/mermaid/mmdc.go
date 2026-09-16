package mermaid

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	xansi "github.com/charmbracelet/x/ansi"
)

// DefaultCommand is the executable md looks for.
const DefaultCommand = "mmdc"

// DefaultTimeout bounds one mmdc run. Rendering a diagram means starting a
// browser, which takes a couple of seconds when it goes well.
const DefaultTimeout = 30 * time.Second

// errTimeout marks a rendering that ran out of time, as opposed to one mmdc
// refused. The difference matters: a refusal is worth remembering, a timeout is
// not.
var errTimeout = errors.New("timed out")

// ErrNoTool reports that mmdc is not on the PATH.
var ErrNoTool = errors.New("mmdc is not installed")

// Tool renders mermaid diagrams with mmdc, keeping the SVG it produces.
//
// The SVG is what is cached, not the drawing: the drawing depends on the width
// of the terminal and the colours in use, while the SVG depends only on the
// diagram. Caching the SVG means a terminal resize costs nothing, and a document
// read a second time draws instantly.
//
// A cached diagram is kept until mmdc itself changes: the entry's timestamp is
// compared with the executable's, so an upgrade redraws rather than serving a
// stale picture. Asking mmdc for its version would say the same thing more
// precisely, and cost a process spawn on every run - four hundred milliseconds of
// browser runtime startup - which is not worth it.
type Tool struct {
	// Command is the executable to run, and defaults to mmdc.
	Command string
	// Timeout bounds one invocation, and defaults to DefaultTimeout.
	Timeout time.Duration
	// CacheDir is where SVGs are kept. An empty value uses the user's cache
	// directory, the same place the palette probe puts its findings.
	CacheDir string
	// Concurrency is how many mmdc processes may run at once. Each one starts a
	// browser, so this stays small.
	Concurrency int
	// OnFirstRun, if set, is called just before the first diagram is drawn. A
	// cold run starts a browser per diagram, which takes seconds, so the caller
	// can say what the wait is for.
	OnFirstRun func()
	// Progress, if set, is told about each diagram as it is drawn. See Progress
	// for the order the events arrive in.
	Progress func(Progress)

	firstRun sync.Once
	// path is the resolved mmdc executable, and resolvedAt is when it was last
	// looked up, so that a long run notices a reinstall.
	pathOnce sync.Once
	path     string
	pathErr  error
	// failures counts diagrams mmdc could not render, so the caller can mention
	// it once rather than once per fence.
	failureMu sync.Mutex
	failures  []error
}

// Available reports whether the tool can be run.
func (t *Tool) Available() bool {
	_, err := t.lookPath()
	return err == nil
}

// cmd returns the command to run.
func (t *Tool) cmd() string {
	if t.Command != "" {
		return t.Command
	}
	return DefaultCommand
}

// lookPath resolves the mmdc executable, once.
func (t *Tool) lookPath() (string, error) {
	t.pathOnce.Do(func() {
		path, err := exec.LookPath(t.cmd())
		if err != nil {
			t.pathErr = fmt.Errorf("%w: %s", ErrNoTool, t.cmd())
			return
		}
		t.path = path
	})
	return t.path, t.pathErr
}

// Failures returns the diagrams that could not be rendered, once each.
func (t *Tool) Failures() []error {
	t.failureMu.Lock()
	defer t.failureMu.Unlock()
	out := t.failures
	t.failures = nil
	return out
}

// Progress is one step in drawing a document's diagrams.
//
// A diagram that has to be drawn is reported twice, once as it starts and once
// as it finishes, so a caller can show a diagram that is taking a while. A
// diagram that came from the cache is reported once, with Cached set: reporting
// the instant round trip twice would double the noise for no information.
type Progress struct {
	// Index is the diagram's position in the batch, counting from one.
	Index, Total int
	// Label names the diagram, such as "graph TD".
	Label string
	// Started is true as a diagram begins to be drawn, and false as it ends.
	Started bool
	// Cached is set when the diagram came from the cache, in which case this is
	// the only event for it.
	Cached bool
	// Took is how long a diagram that had to be drawn took.
	Took time.Duration
	// Err is why a diagram could not be drawn.
	Err error
}

// Diagram renders one diagram to SVG, from the cache when it is there.
func (t *Tool) Diagram(ctx context.Context, source string) ([]byte, error) {
	svg, _, err := t.diagram(ctx, source)
	return svg, err
}

// diagram renders one diagram, saying whether the cache answered.
//
// Both outcomes are cached. A diagram mmdc refuses is refused in the same way
// every time, and finding that out costs a browser launch, so remembering the
// refusal saves a second or so on every read of the document. Editing the
// diagram changes its key, and upgrading mmdc expires the entry, so a refusal is
// never permanent.
func (t *Tool) diagram(ctx context.Context, source string) (svg []byte, cached bool, err error) {
	toolPath, pathErr := t.lookPath()
	key := t.key(source)
	entry := cachePath(t.CacheDir, key)

	if data, ok := readIfFresh(entry, toolPath); ok {
		return data, true, nil
	}
	if msg, ok := readIfFresh(entry+failureSuffix, toolPath); ok {
		return nil, true, errors.New(string(msg))
	}
	if pathErr != nil {
		return nil, false, pathErr
	}

	svg, err = t.run(ctx, source)
	if err != nil {
		// A timeout says nothing about the diagram: the machine was busy. Keep
		// the question open rather than remembering a refusal that may have been
		// about something else entirely.
		if !errors.Is(err, errTimeout) {
			writeFile(entry+failureSuffix, []byte(err.Error()))
			removeFile(entry)
		}
		return nil, false, err
	}
	removeFile(entry + failureSuffix)
	writeFile(entry, svg)
	return svg, false, nil
}

// key is the cache key for a diagram. It is the diagram alone: whether the entry
// is still good is decided by timestamps, not by asking mmdc anything.
func (t *Tool) key(source string) string {
	sum := sha256.Sum256([]byte(cacheVersion + "\x00" + source))
	return hex.EncodeToString(sum[:])
}

// Diagrams renders several diagrams, bounding how many run at once: each mmdc
// invocation starts a browser, and a document with twenty diagrams should not
// start twenty of them.
func (t *Tool) Diagrams(ctx context.Context, sources []string) [][]byte {
	out := make([][]byte, len(sources))
	if len(sources) == 0 {
		return out
	}
	if t.OnFirstRun != nil {
		t.firstRun.Do(t.OnFirstRun)
	}
	workers := t.Concurrency
	if workers < 1 {
		workers = 4
	}
	if workers > len(sources) {
		workers = len(sources)
	}

	var (
		wg   sync.WaitGroup
		next sync.Mutex
		at   int
	)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				next.Lock()
				i := at
				at++
				next.Unlock()
				if i >= len(sources) {
					return
				}
				t.draw(ctx, i, sources[i], out)
			}
		}()
	}
	wg.Wait()
	return out
}

// draw renders one diagram of a batch and reports on it.
func (t *Tool) draw(ctx context.Context, i int, source string, out [][]byte) {
	event := Progress{Index: i + 1, Total: len(out), Label: label(source), Started: true}

	start := time.Now()
	svg, cached, err := t.diagram(ctx, source)
	if err != nil {
		t.noteFailure(err)
	}
	out[i] = svg

	if t.Progress == nil {
		return
	}
	if cached {
		// A refusal is cached too, and has to arrive as a failure as well, or
		// the reader would be told nothing about the diagram that is missing.
		t.Progress(Progress{
			Index: event.Index, Total: event.Total, Label: event.Label,
			Cached: true, Err: err,
		})
		return
	}
	t.Progress(event)
	done := event
	done.Started, done.Took, done.Err = false, time.Since(start), err
	t.Progress(done)
}

// label names a diagram for a progress line: the first line of substance, which
// is the diagram type and often its title.
func label(source string) string {
	for _, line := range strings.Split(source, "\n") {
		line = collapseSpace(line)
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}
		return shorten(line, 40)
	}
	return "diagram"
}

// shorten trims a label to a sensible length for a progress line.
func shorten(s string, cells int) string {
	if xansi.StringWidth(s) <= cells {
		return s
	}
	out := make([]rune, 0, cells)
	width := 0
	for _, r := range s {
		w := xansi.StringWidth(string(r))
		if width+w > cells-1 {
			break
		}
		out = append(out, r)
		width += w
	}
	return string(out) + "…"
}

func (t *Tool) noteFailure(err error) {
	t.failureMu.Lock()
	defer t.failureMu.Unlock()
	if len(t.failures) < 3 {
		t.failures = append(t.failures, err)
	}
}

// run invokes mmdc on one diagram.
func (t *Tool) run(ctx context.Context, source string) ([]byte, error) {
	path, err := t.lookPath()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "md-mermaid")
	if err != nil {
		return nil, fmt.Errorf("making a temporary directory: %w", err)
	}
	defer os.RemoveAll(dir) //nolint:errcheck // best effort

	in := filepath.Join(dir, "diagram.mmd")
	out := filepath.Join(dir, "diagram.svg")
	if err := os.WriteFile(in, []byte(source), 0o600); err != nil {
		return nil, fmt.Errorf("writing the diagram: %w", err)
	}

	timeout := t.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, "-i", in, "-o", out, "-q")
	// mmdc writes its progress to stderr; keep it, and keep it out of md's
	// output, which has to stay a document.
	var stderr strings.Builder
	cmd.Stderr = &stderr
	cmd.Stdout = nil
	// mmdc starts a browser, and a browser can outlive its parent: without this
	// a timeout would be cancelled and then wait for the browser to let go of
	// the pipe.
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w after %s", errTimeout, timeout)
		}
		return nil, fmt.Errorf("rendering the diagram: %w: %s",
			err, firstLine(stderr.String()))
	}
	svg, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("reading the rendered diagram: %w", err)
	}
	return svg, nil
}

// key is the cache key for a diagram: the diagram itself and the version of

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return "no output"
}

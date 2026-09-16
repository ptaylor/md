# md

<p align="center">
  <img src="assets/logo.svg" alt="md: a Markdown document rendered in a terminal" width="292" height="160">
</p>

Render Markdown in the terminal, in the colours you already use.

    ┌───────────┐                ╭───────────╮
    │ # Title   │     ──▶        │ ▌ Title   │
    │ > quote   │                │ ▌ quote   │
    │ - item    │                │ • item    │
    │ ---       │                │ ───────── │
    └───────────┘                ╰───────────╯

```sh
md README.md
```

`md` renders a Markdown file into a full-screen pager with `less`-style keys. It
draws headings, tables, code and diagrams with box drawing characters, and picks
its palette from your terminal: it asks the terminal for its own 16 colours and
background, and refers to those colours by index on terminals that do not answer,
so the output matches your theme either way.

## Install

```sh
./install.sh              # build and link into ~/bin
./install.sh --prefix /usr/local/bin
./install.sh --uninstall
```

The script builds `bin/md` in this checkout and symlinks it into `--prefix`
(`~/bin` by default), so `git pull && ./install.sh` updates an installed copy. It
tells you if the prefix is not on your `PATH`.

Building needs **Go 1.25 or newer** (glamour v2 requires it). `install.sh` checks
the version and explains what to do if it is too old.

## Usage

```
md [options] FILE.md
md [options] DIRECTORY    choose a Markdown file from one
md [options] -            read from standard input
```

`md` also reads standard input when given no arguments and standard input is a
pipe, so `cat notes.md | md` and `md < notes.md` both work.

| Option | Default | Description |
| ------ | ------- | ----------- |
| `-w`, `--width N` | terminal width | render width in columns |
| `--max-width N` | `100` | cap the content width so long lines stay readable |
| `--theme NAME` | `auto` | `auto`, `dark`, `light` or `mono` |
| `--pager NAME` | `auto` | `auto`, `builtin`, `less` or `none` |
| `--mermaid NAME` | `auto` | `auto` draws diagrams with `mmdc`, `box` frames the source, `off` treats it as code |
| `--links NAME` | `auto` | `auto`, `inline`, `both` or `plain` |
| `--ascii` | off | ASCII glyphs instead of box drawing characters |
| `--no-color` | off | disable colour (also honours `NO_COLOR`) |
| `--color` | off | force colour even when output is not a terminal |
| `--no-probe` | off | do not query the terminal for its palette |
| `--refresh-palette` | off | ignore the cached palette and query the terminal again |
| `--clear-cache` | off | remove the cached palette and diagrams |
| `--version` | | print the version |
| `-h`, `--help` | | print help |

When the output is not a terminal (a pipe or a redirect) `md` prints the
document without colour instead of starting the pager. `--color` overrides that,
so `md --color README.md | less -R` works.

### Choosing a file

Point `md` at a directory rather than a document and it offers the Markdown
files in it:

```sh
md .
```

The files are listed by name, matching `.md` whatever the case of the
extension, and subdirectories are not descended into. `↑`/`↓`, or `j`/`k`,
move; `enter` reads the file; `q` leaves the list. After reading, `q` returns
to the list, so a directory is somewhere you can browse rather than a choice
you make once. The last row names the directory, where you are in the list, and
the keys.

With no terminal to choose on - the output piped or redirected, or
`--pager=none` - `md` prints the list instead of asking, which is useful on its
own. A directory holding no Markdown files is reported rather than shown empty.

### Keys

| Key | Action |
| --- | ------ |
| `j` / `k`, `↓` / `↑` | scroll a line |
| `enter`, `ctrl-j` | scroll down a line |
| `space`, `f`, `pgdn` / `b`, `pgup` | scroll a page |
| `d`, `ctrl-d` / `u`, `ctrl-u` | scroll half a page |
| `g`, `home` / `G`, `end` | jump to the top or bottom |
| `/` | search forwards |
| `n` / `N` | next / previous match |
| mouse wheel | scroll |
| `?` | toggle the help line |
| `q`, `esc`, `ctrl-c` | quit |

Set `--pager less` (or `MD_PAGER`, `PAGER`) to use your own pager instead:
`md` checks `$MD_PAGER`, then `$PAGER`, then falls back to `less -R`.

### Search

`/` opens a prompt at the bottom of the screen: type a string and press `enter`.
The string is taken literally, so what you type is what is looked for rather than
a pattern, and the case is ignored. The search starts from the top line of the
screen and takes you to the hit it finds, marking the matches as you go; `n` and
`N` step forwards and backwards through them.

Running off the end wraps to the top and says so, so a search either finds
something or tells you it did not. `esc` closes the prompt without searching, and
`enter` with nothing typed calls the search off and takes the marks away.

What marks a match is reverse video, and underline for the one in hand. Those are
attributes rather than colours, so they read on any theme and leave the syntax
colours underneath them alone. They are also attributes `md`'s own output does not
lean on: it never sets reverse video, and its underlined links carry a colour in
the same sequence, so undoing a mark cannot take anything off the document.

## How the colours work

`md` never hard-codes a palette. At startup it queries the terminal (OSC 4 for the
16 ANSI slots, OSC 10 and 11 for the default foreground and background) and
renders with the exact colours it is given. That result is cached for an hour, so
the query costs nothing after the first run.

Both caches — the palette and the diagrams — live in one directory:
`$XDG_CACHE_HOME/md` when that variable is set, and otherwise the directory the
platform designates for cache files, which is `~/Library/Caches/md` on macOS and
`~/.cache/md` on Linux. `--clear-cache` empties it, with or without a document
to render.

If the terminal does not answer — Apple Terminal does not implement these queries
— `md` refers to the ANSI slots by index instead, and the terminal paints them
with whatever scheme is active. Either way, changing your terminal's colour
preset changes `md`'s output.

Text uses the terminal's own default foreground rather than a chosen white or
grey, and italics are left alone because several terminals render them as
reverse video.

Code is coloured from the same slots. Every colour comes in a bright and a
deeper form, and which one shows depends on the background: a dark background
swallows the deeper half and a light one washes out the bright half, so
highlighting takes whichever form stays legible, and code with no colour of its
own keeps the terminal's default foreground. Inline code is marked by its colour
alone, on the terminal's own background — a shade of the background under text
that was chosen to sit on the background costs contrast rather than adding
definition.

If a terminal answers only partially, `md` keeps to colour indices, because
guessing RGB values for the slots it did not receive would be worse than letting
the terminal fill them in.

## Configuration

`md` reads `~/.config/md/config.toml` (`$XDG_CONFIG_HOME/md/config.toml` if set,
or `$MD_CONFIG` to point somewhere else):

```toml
# md's settings are a flat list of key = value pairs.
width = 0            # 0 means "use the terminal width"
max_width = 100
theme = "auto"       # auto, dark, light or mono
pager = "auto"       # auto, builtin, less or none
mermaid = "auto"     # auto, box or off
mermaid_cmd = ""     # mmdc to run, if not the one on the PATH
links = "auto"       # auto, inline, both or plain
ascii = false
no_color = false
no_probe = false
```

Environment variables override the file, and command line flags override both:

| Variable | Sets |
| -------- | ---- |
| `MD_WIDTH`, `MD_MAX_WIDTH` | render width, maximum content width |
| `MD_THEME`, `MD_PAGER_MODE`, `MD_MERMAID`, `MD_LINKS` | the matching option |
| `MD_MERMAID_CMD` | the `mmdc` executable to run |
| `MD_ASCII`, `MD_NO_COLOR`, `MD_NO_PROBE` | the matching switch |
| `MD_PAGER` | the external pager command |
| `MD_HYPERLINKS=0` | do not use OSC 8 hyperlinks even if supported |
| `NO_COLOR` | disable colour |

## What it renders

Headings, paragraphs, emphasis, strikethrough, nested lists, ordered lists, task
lists, block quotes, horizontal rules, tables, links (with OSC 8 hyperlinks on
terminals that support them), inline code, fenced and indented code, footnotes,
definition lists, emoji shortcodes, and YAML or TOML front matter as a metadata
panel.

Code fences are drawn as labelled panels with real syntax highlighting, and lines
too long for the panel are wrapped with a `↳` continuation marker rather than
truncated. Mermaid fences are drawn as diagrams when `mmdc` is installed, and
otherwise framed as source.

## Mermaid diagrams

A mermaid fence is drawn as a diagram if `mmdc`, the mermaid command line
renderer, is installed. Nothing has to be configured:

```sh
npm install -g @mermaid-js/mermaid-cli
```

`mmdc` is optional and `md` never needs it. It is used as a **layout engine**
only: mermaid decides where the boxes go and how the lines between them run, md
reads that geometry back out of the SVG and draws it itself. So a diagram is
text, in the terminal's own colours, and it scrolls with the rest of the
document - none of which an image inside a pager can do.

The SVG `mmdc` produces is cached next to the palette, keyed by the diagram, and
kept until `mmdc` itself changes: an entry is aged against the executable's
timestamp, so upgrading `mmdc` redraws rather than showing a stale picture.
Asking `mmdc` for its version would say the same thing more precisely, and cost a
process spawn on every run — four hundred milliseconds of browser runtime
startup — so `md` compares timestamps instead.

A diagram `mmdc` refuses is remembered as well, so a fence it cannot draw costs
nothing after the first read: the drawing is not attempted again. Editing the
diagram, or upgrading `mmdc`, asks again. A timeout is not treated as a refusal,
because a machine being busy says nothing about the diagram.

Drawing a diagram means starting a browser, about two seconds per diagram, so the
first read of a document is slow and every read after it is instant.
`--clear-cache` throws it away.

What `md` will not do is draw a diagram it cannot draw properly. A diagram with
a node it cannot read, or one that will not fit the width available, is shown as
source. Wide diagrams are drawn as small as the text allows, and labels are
wrapped rather than truncated, so nothing is quietly lost - a picture that drops
a step is worse than the source it came from.

Diagram types `md` cannot draw yet - sequence, pie, class, state, ER - fall back
to the framed source, as do the colours a diagram asks for with `style` or
`classDef`: the terminal's palette wins.

A node's outline is read from the shape mermaid drew: a rectangle, a rounded box,
a circle, and a point for a decision. Mermaid draws several shapes as a polygon -
a subroutine with its bars, a hexagon - so `md` tells them apart by how much of
their box they fill, and draws the ones it has no outline for as their bounding
box. A plain box for a subroutine keeps the label and loses only the decoration,
which is a better bargain than a diamond the label does not fit inside.

### Known limitations

- **Mermaid support is partial.** Flowcharts are drawn when `mmdc` is installed.
  Sequence, pie, class, state and ER diagrams are shown as source. Shapes without
  an outline of their own - a subroutine, a hexagon - are drawn as plain boxes.
  Their routing is mermaid's, so a crowded diagram can still draw lines through a
  box.
- **Images are placeholders.** `![alt](path)` renders as a caption and a path.
  Inline images need the kitty or iTerm2 graphics protocol, and they do not
  survive a scrolling viewport.
- **Indented (four-space) code blocks** are not drawn as panels; only fenced
  blocks are. They still render, but without syntax highlighting.
- **Footnotes are numbered by the order they are first referenced**, and
  unreferenced definitions are dropped. A note containing several paragraphs is
  flattened onto one line.
- **Maths is not rendered**; `$...$` and `$$...$$` pass through as text.
- Footnotes, front matter and code panels are handled by md's own source
  pre-pass. A fence md cannot parse (an unterminated one, for instance) is left
  for glamour to render rather than disappearing.

### Terminals

Tested on Apple Terminal and iTerm2. In iTerm2 the exact palette is read back, so
code highlights match your preset; Apple Terminal falls back to ANSI indices and
link URLs are printed after the link text, because OSC 8 hyperlinks are ignored
there and the URL would otherwise be lost. `TERM=dumb` or a non-UTF-8 locale
switches to ASCII glyphs automatically.

## Development

```sh
go build ./...           # build everything
go test ./...            # unit, layout and golden tests
go vet ./...
go test ./internal/render/ -count=1 -update=true   # refresh golden files
./install.sh             # rebuild the linked binary
```

The renderer is a pipeline of small packages:

| Package | Responsibility |
| ------- | -------------- |
| `internal/term` | size, colour profile, background, palette probe and cache |
| `internal/theme` | the 16 slots, plus panel, rule, dim and heading colours |
| `internal/render` | style config, code panels, metadata, footnotes, links |
| `internal/pager` | viewport pager and external pager |

Two constraints in the dependencies shape the rendering code, and both are worth
knowing before changing it:

1. glamour buffers the whole document and flushes it only at the end, so a node
   renderer that writes straight to the output during the walk lands *above* the
   document. md therefore leaves a one-line sentinel where each code block
   belongs, lets glamour render it in place, and swaps that line for the finished
   panel afterwards (`substituteBlocks` / `expandBlocks`). The sentinel inherits
   the block's indentation, so nested blocks stay nested.
2. glamour v2 registers the footnote node kinds but implements none of them: it
   prints `Warning: unhandled element` **to stdout**, which would corrupt both
   piped output and the pager's screen. Footnotes are therefore extracted from
   the source before parsing (`extractFootnotes`).

`internal/render/testdata/` holds the shared fixture, and the golden files cover
the palette tiers, ASCII mode, a narrow terminal and the link modes. The layout
test renders at six widths and asserts that no line exceeds the terminal width.

## Licence

MIT. See `LICENSE`.


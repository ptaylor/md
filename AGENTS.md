# Working in md

## 1. What md is for

`md` renders a Markdown file in the terminal, so that reading a document is a
pleasure rather than a chore. Judge every change against three qualities, in this
order.

- **Beautiful.** Output is drawn, not dumped: box drawing characters, aligned
  tables, syntax-coloured code, mermaid diagrams drawn as text. A document
  rendered by `md` should look deliberate from its first line to its last.
- **Clear.** Legibility beats decoration, always. Where a flourish costs a word, a
  border or a line of contrast, it is the flourish that goes. Nothing is dropped
  quietly either: something `md` cannot draw properly - a diagram with a node it
  cannot place, a label that will not fit - is shown as source instead, because a
  picture that is missing a step is a lie about the document.
- **Complementary.** The palette is the terminal's own, never a colour `md`
  invented. `md` asks the terminal for its 16 colours and background (OSC
  4/10/11), and refers to those colours by ANSI index on terminals that do not
  answer, so the output matches whatever theme the reader has configured. Text is
  drawn in the terminal's default foreground, and where a bright and a deeper form
  of a colour both exist, which one is used depends on the background: a change
  that hard-codes a colour, or that keeps a colour a document asked for, is wrong
  even when it looks nice on your terminal. See `internal/theme`.

## 2. Go

- Build for the Go version in `go.mod`; `install.sh` checks it and explains
  itself if it is too old.
- Prefer the standard library. Add a dependency only when it earns its place, and
  say so in the commit: the ones here are the Markdown renderer and its parser,
  the pager runtime, and the syntax highlighter.
- Return errors wrapped with `%w`; no `panic`, no `log.Fatal`. Diagnostics belong
  to `main`, on standard error, where they cannot end up inside the document:
  nothing under `internal/` writes a status line of its own, and the only other
  thing md puts on the wire is the colour query in `internal/term`.
- Doc comments start with the identifier's name and say why the thing exists, not
  what it does. A comment that repeats the code is worse than no comment; a
  comment that records a measurement, a constraint or a bug that was once there is
  worth keeping.
- Keep packages single-purpose (`internal/mermaid` draws diagrams, `internal/cache`
  finds the cache directory) and keep the pipeline visible in `main.go`.

## 3. Format and tidy before committing

```sh
goimports -l -w .   # gofmt, with the import block fixed as well
go vet ./...
```

`gofmt -l .` must print nothing. If `goimports` is not installed:

```sh
go install golang.org/x/tools/cmd/goimports@latest
```

## 4. Run the tests before committing

```sh
go test ./...
```

All of them, and not only the package that was touched: the renderer, the pager
and the palette all read each other. Tests must not need `mmdc`, a terminal or
anything else installed - a test that shells out uses a stub on `PATH`, and
anything else it needs is committed as a fixture in `testdata/`.

## 5. New functionality means new tests

Behaviour arrives with the test that would have failed without it. Test the
property rather than the current output: that every word of a label comes out, that
a line fits the width it was given, that a palette stays legible on both
backgrounds. Say in the comment what property the test protects and why it is
worth protecting - the next reader is not the only one who will change this code.

The documents in `examples/` are the corpus a feature is developed against, and
their headings say what the renderer is expected to do with each one. A new case
usually starts as a section there.

## 6. Commits

Subject first, in the imperative and in lower case after the verb ("Draw a pointed
box from the room its label really has"). Then a body explaining the reasoning:
what was wrong, what the alternatives were, and what was measured. Line the body
at 80 columns.

Name the assistant and the model in a trailer at the end:

```sh
git commit --trailer "Assisted-by=GitHub Copilot (DeepSeek V4 Flash)"
```

The trailer is the record of what wrote the change, so it names the version as well
as the model, and it is a trailer rather than prose so that it can be found:

    Assisted-by: GitHub Copilot (DeepSeek V4 Flash)

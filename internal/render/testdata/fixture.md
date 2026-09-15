---
title: Fixture
author: md test suite
tags: [render, golden]
---

# Heading one

A paragraph with **bold**, _emphasis_, ~~strikethrough~~, `inline code` and a
[link](https://example.com/very/long/path?query=value).

## Lists

- first bullet
  - nested bullet
    - deeper still
- second bullet with a very long line that has to be wrapped by the renderer to
  fit within the configured width, because that is what readers expect

1. ordered one
2. ordered two
   - nested under ordered

- [x] finished task
- [ ] pending task

> A block quote, which should be marked down its left edge.
>
> > And a nested quote inside it.

Term
: the definition of the term

Other term
: another definition

## Table

| Option | Type | Default | Description |
| ------ | :--: | ------: | ----------- |
| `--width` | int | terminal | render width in columns |
| `--theme` | string | auto | dark, light or mono |

## Code

```go
package main

// A comment.
func main() {
	println("hello")
}
```

```python
def a_really_long_line_that_will_not_fit_inside_any_reasonable_terminal_width(at_all):
    return "wrapped with a continuation marker"
```

```unknown-language
no lexer for this one
```

```
plain fence with no language
```

An indented code block:

    indented line one
    indented line two

## Mermaid

```mermaid
graph TD
    A --> B
```

## Extras

---

Text with a footnote[^note] and a second reference to it[^note], plus one more[^other].

[^note]: The first note, which is long enough that it should wrap onto a second
    line when the terminal is narrow.

[^other]: The second note.

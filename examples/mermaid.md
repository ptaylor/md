---
title: Mermaid samples
tags: [mermaid, diagrams]
---

# Mermaid samples

A corpus for the mermaid work. Every diagram is deliberately small, so it
isolates one feature that has to be drawn, and the note under each heading says
what the renderer is expected to do with it.

Until diagrams are drawn, each fence appears as a dimmed source panel labelled
`mermaid`.

## Pie chart

The easiest case, and a good first win: lengths derived from percentages, with no
routing at all.

```mermaid
pie title Where the time goes
    "syntax highlighting" : 45
    "layout" : 30
    "palette probe" : 15
    "everything else" : 10
```

## Flowchart, top to bottom

The baseline: a branch, a decision and edge labels. Expect three levels, `{...}`
drawn as a diamond, and `yes`/`no` placed beside their edges.

```mermaid
graph TD
    A[md README.md] --> B{terminal has colour?}
    B -- yes --> C[ask the terminal]
    B -- no --> D[use ANSI indices]
    C --> E[render]
    D --> E
```

## Flowchart, left to right

The same shape in the other direction: layout must follow the declared direction
rather than always stacking downwards.

```mermaid
graph LR
    Read[read file] --> Split[split front matter]
    Split --> Render[render]
    Render --> Page[page]
```

## Node shapes and edge styles

Stadium, subroutine, cylinder and circle nodes, plus dotted and thick edges.

```mermaid
graph TD
    Id([start]) --> Proc[[run]]
    Proc --> Store[(cache)]
    Proc -. cache hit .-> Done((done))
    Proc ==> Retry[retry]
    Retry --> Proc
```

## A cycle

Edges that go back on themselves: the router must terminate.

```mermaid
graph TD
    A[read] --> B[parse]
    B --> C{valid?}
    C -- no --> B
    C -- yes --> D[render]
    D --> A
```

## Long labels

Text wider than the terminal: the box has to grow or the label has to wrap,
without the frame coming apart.

```mermaid
graph LR
    A[A node label long enough that it cannot fit in a narrow terminal] --> B[short]
```

## Subgraph

A container drawn around part of the graph.

```mermaid
graph TD
    subgraph render
        A[goldmark] --> B[glamour]
    end
    B --> C[panels]
```

## Sequence diagram

Participants, messages both ways, a note and a loop.

```mermaid
sequenceDiagram
    participant U as User
    participant M as md
    participant T as Terminal
    U->>M: md README.md
    M->>T: query palette (OSC 4/10/11)
    T-->>M: colours
    Note over M,T: cached for an hour
    loop for each block
        M->>M: render
    end
    M-->>U: page
```

## Styling directives

`style` and `classDef` carry colours md will ignore: the terminal's palette wins,
and the diagram still has to read.

```mermaid
graph LR
    A[plain] --> B[styled]
    style B fill:#f9f,stroke:#333,stroke-width:4px
    classDef loud fill:#ff0
    class B loud
```

## Unicode and emoji

Width is measured in cells, not bytes: the frame must stay square.

```mermaid
graph LR
    A["café"] --> B["→ arrow"]
    B --> C["日本語のラベル"]
```

## Comments and blank lines

`%%` comments and stray blanks must be ignored rather than drawn.

```mermaid
%% this is a comment
graph TD

    A[start] --> B[end]
    %% and so is this
```

## Not a diagram

An unparseable body: give up and fall back to the dimmed source panel rather
than drawing nonsense.

```mermaid
this is not a diagram at all
    ??? --> !!!
```

## A diagram with no language

For contrast, a plain fence: not detected as mermaid, drawn as an ordinary code
panel.

```
graph TD
    A --> B
```

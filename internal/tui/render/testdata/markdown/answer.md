# Plan for the cache

The render cache keeps **finished** items per width, and the `Screen` function draws from the bottom up. This answer walks through the change, file by file, with the *reasons* for each step.

## Step 1: the parser

Each step keeps the renderer pure: no I/O, and nothing shared between two caches. See [the design](https://example.com/docs/design/markdown.md) and `internal/tui/render/markdown.go` for details.
A second line of the same paragraph follows the first one directly.

- Parse the text with goldmark and walk the tree.
- Keep each finished block's lines, keyed by its source and the width.
  - Nested items hang under their parent.
  - [x] A done task
  - [ ] An open task
- Re-render only the last block while the answer streams.

```go
// render draws one block at width w.
func (r *Renderer) render(src []byte, n ast.Node, w int) []string {
	var out []string
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		out = append(out, r.block(src, c, w)...)
	}

	return out
}
```

| Case | Before | After | Note |
| --- | ---: | ---: | :---: |
| cold render | 1.2 ms | 0.9 ms | ok |
| warm render | 400 µs | 2 µs | cached |
| streaming update | 1.1 ms | 30 µs | last block only |

> A quote with **bold** text, and a second sentence that is long enough to wrap at a narrow width.

```bash
go test -race ./...
golangci-lint run ./...
```

1. First numbered step.
2. Second numbered step, with ~~struck~~ text.

---

## Step 2: the blocks

Each step keeps the renderer pure: no I/O, and nothing shared between two caches. See [the design](https://example.com/docs/design/markdown.md) and `internal/tui/render/markdown.go` for details.
A second line of the same paragraph follows the first one directly.

- Parse the text with goldmark and walk the tree.
- Keep each finished block's lines, keyed by its source and the width.
  - Nested items hang under their parent.
  - [x] A done task
  - [ ] An open task
- Re-render only the last block while the answer streams.

```go
// render draws one block at width w.
func (r *Renderer) render(src []byte, n ast.Node, w int) []string {
	var out []string
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		out = append(out, r.block(src, c, w)...)
	}

	return out
}
```

| Case | Before | After | Note |
| --- | ---: | ---: | :---: |
| cold render | 1.2 ms | 0.9 ms | ok |
| warm render | 400 µs | 2 µs | cached |
| streaming update | 1.1 ms | 30 µs | last block only |

> A quote with **bold** text, and a second sentence that is long enough to wrap at a narrow width.

```bash
go test -race ./...
golangci-lint run ./...
```

1. First numbered step.
2. Second numbered step, with ~~struck~~ text.

---

## Step 3: the tables

Each step keeps the renderer pure: no I/O, and nothing shared between two caches. See [the design](https://example.com/docs/design/markdown.md) and `internal/tui/render/markdown.go` for details.
A second line of the same paragraph follows the first one directly.

- Parse the text with goldmark and walk the tree.
- Keep each finished block's lines, keyed by its source and the width.
  - Nested items hang under their parent.
  - [x] A done task
  - [ ] An open task
- Re-render only the last block while the answer streams.

```go
// render draws one block at width w.
func (r *Renderer) render(src []byte, n ast.Node, w int) []string {
	var out []string
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		out = append(out, r.block(src, c, w)...)
	}

	return out
}
```

| Case | Before | After | Note |
| --- | ---: | ---: | :---: |
| cold render | 1.2 ms | 0.9 ms | ok |
| warm render | 400 µs | 2 µs | cached |
| streaming update | 1.1 ms | 30 µs | last block only |

> A quote with **bold** text, and a second sentence that is long enough to wrap at a narrow width.

```bash
go test -race ./...
golangci-lint run ./...
```

1. First numbered step.
2. Second numbered step, with ~~struck~~ text.

---

## Step 4: the highlighting

Each step keeps the renderer pure: no I/O, and nothing shared between two caches. See [the design](https://example.com/docs/design/markdown.md) and `internal/tui/render/markdown.go` for details.
A second line of the same paragraph follows the first one directly.

- Parse the text with goldmark and walk the tree.
- Keep each finished block's lines, keyed by its source and the width.
  - Nested items hang under their parent.
  - [x] A done task
  - [ ] An open task
- Re-render only the last block while the answer streams.

```go
// render draws one block at width w.
func (r *Renderer) render(src []byte, n ast.Node, w int) []string {
	var out []string
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		out = append(out, r.block(src, c, w)...)
	}

	return out
}
```

| Case | Before | After | Note |
| --- | ---: | ---: | :---: |
| cold render | 1.2 ms | 0.9 ms | ok |
| warm render | 400 µs | 2 µs | cached |
| streaming update | 1.1 ms | 30 µs | last block only |

> A quote with **bold** text, and a second sentence that is long enough to wrap at a narrow width.

```bash
go test -race ./...
golangci-lint run ./...
```

1. First numbered step.
2. Second numbered step, with ~~struck~~ text.

---

## Step 5: the benchmarks

Each step keeps the renderer pure: no I/O, and nothing shared between two caches. See [the design](https://example.com/docs/design/markdown.md) and `internal/tui/render/markdown.go` for details.
A second line of the same paragraph follows the first one directly.

- Parse the text with goldmark and walk the tree.
- Keep each finished block's lines, keyed by its source and the width.
  - Nested items hang under their parent.
  - [x] A done task
  - [ ] An open task
- Re-render only the last block while the answer streams.

```go
// render draws one block at width w.
func (r *Renderer) render(src []byte, n ast.Node, w int) []string {
	var out []string
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		out = append(out, r.block(src, c, w)...)
	}

	return out
}
```

| Case | Before | After | Note |
| --- | ---: | ---: | :---: |
| cold render | 1.2 ms | 0.9 ms | ok |
| warm render | 400 µs | 2 µs | cached |
| streaming update | 1.1 ms | 30 µs | last block only |

> A quote with **bold** text, and a second sentence that is long enough to wrap at a narrow width.

```bash
go test -race ./...
golangci-lint run ./...
```

1. First numbered step.
2. Second numbered step, with ~~struck~~ text.

---

That is the whole plan. Autolinks such as https://github.com/yuin/goldmark work too.

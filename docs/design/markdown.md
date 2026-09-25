# Markdown rendering

Ledger item 47: rich text as a core subsystem, designed for speed. A real Markdown parser (GFM: tables, lists, code, quotes, links), tables that fit the width, cached syntax highlighting, and incremental rendering, so a streaming answer re-renders only its last block. Benchmarks gate it.

1. [What uah did before](#what-uah-did-before)
2. [What Codex does](#what-codex-does)
3. [The parser](#the-parser)
4. [Blocks and incremental rendering](#blocks-and-incremental-rendering)
5. [Tables](#tables)
6. [Highlighting](#highlighting)
7. [The look](#the-look)
8. [Performance budget and gates](#performance-budget-and-gates)
9. [As built](#as-built)
10. [Open](#open)

## What uah did before

`internal/tui/render/markdown.go` was about 150 lines of line-based regular expressions: fenced code highlighted with chroma on the band, `code`, bold, italic, links, headings, lists, quotes, and rules. It had no tables, no nesting (a nested list was its source indentation), and no notion of a block: every call rendered the whole text. The call site is one method, `(*Styles).markdownLines(text, w, first, rest)`, used for agent messages in both views.

Measured on `internal/tui/render/testdata/markdown/answer.md` (215 lines, 6.5 KB: headings, paragraphs, nested and task lists, five Go and five shell code blocks, five tables, quotes), width 100, Apple M4 Max, Go 1.27.1:

| Case | Time | Allocations |
| --- | --- | --- |
| One render | 2.39 ms | 15,061 (866 KB) |
| Streaming it 12 bytes at a time (543 updates, each rendering the whole text so far) | 631 ms in all, 1.16 ms per update | 3.79 M (218 MB) |

The streaming cost grows with the answer: each delta re-parses, re-wraps, and re-highlights everything above it.

## What Codex does

Checked against Codex rust-v0.156.1. Paths are under `codex-rs/tui/src/`.

- **Parser.** `pulldown-cmark` with strikethrough and tables (`markdown_render/streaming.rs:47-49`); task lists are off. `markdown_render.rs` walks its events and writes styled `ratatui` lines: headings keep their `#` marks, quotes get `> `, list markers are `-` and `N.`, soft breaks stay line breaks (`soft_break`, `markdown_render.rs:819-836`), and a blank line separates top-level blocks (`needs_newline`).
- **Streaming.** `markdown_stream.rs` collects deltas and commits source only up to the last newline (`commit_complete_source`), so a half line never renders. `streaming/render.rs` keeps a stable prefix: "Completed top-level blocks are retained while the final block stays mutable" (`StreamingRender`, lines 1-4 and 20-40). Each commit renders only the source after the stable boundary, moves every block but the last into the stable prefix, and re-renders the last (`append`, lines 128-235). A width change re-renders the whole source (`recompute`). Reference-style link definitions force a full render, because a definition changes links in other blocks (`has_reference_link_definition`). An open code fence is appended line by line with a kept highlighter state (`streaming/code_fence.rs`, `render/highlight_streaming.rs`). A table stays mutable until it ends, since a new row can widen earlier columns (`streaming/table_holdback.rs`).
- **Tables.** `render_table_lines` (`markdown_render.rs:1188-1309`): column widths start at the widest cell and shrink by priority until they fit, with a floor per column (`compute_column_widths`, `shrink_columns`); columns are classed as narrative, token-heavy, or compact, and token-heavy ones give up width first. Columns are separated by two spaces with one cell of padding; the header is bold in the theme's type color, a `━` rule follows it, and a `─` rule separates body rows. When even 3 cells per column do not fit, or enough rows would split words into fragments, the rows render as key/value records instead (`markdown_render/table_key_value.rs`: `label  value`, or the label above an indented value when narrow, with a muted rule between records).
- **Highlighting.** syntect with two-face's grammars (`render/highlight.rs`). The grammar set is a process-wide `OnceLock`; a theme revision counter invalidates rendered caches. A fence without a language, or with an unknown one, is plain text (`find_syntax` returns `None`). Inputs over 512 KB, 10,000 lines, or with a line over 4 KiB are not highlighted (`MAX_HIGHLIGHT_BYTES`, `MAX_HIGHLIGHT_LINES`, `MAX_HIGHLIGHT_LINE_BYTES`).

## The parser

[goldmark](https://github.com/yuin/goldmark) v1.8.6 with its GFM extensions (tables, strikethrough, autolinks, task lists). It is CommonMark compliant, pure Go, has no dependencies of its own, and is what glamour is built on. Since v1.8 every block node carries its source offset (`Node.Pos`, set in `parser.go` when the block opens), which is what the block split needs.

uah walks goldmark's tree with its own terminal renderer rather than using [glamour](https://github.com/charmbracelet/glamour):

- **Speed.** On a 2.2 KB, 152-line document, goldmark parses in 62 µs with 646 allocations; glamour v2.0.1 renders it in 6.0 ms with 232,140 allocations (`go test -bench`, same machine), about 100 times slower.
- **Weight.** glamour pulls in 39 modules, among them bluemonday, gorilla/css, and goldmark-emoji; goldmark adds one.
- **Control.** glamour styles with its own JSON style sheets and has no block or streaming API. The look here is uah's (the band, the dim quote bar, `•` bullets), the theme's `Styles` color it, and the renderer must know where blocks start.

## Blocks and incremental rendering

A message is a sequence of top-level blocks: goldmark's document children. Once a later block has started, an earlier one cannot change: CommonMark closes a top-level block before the next one opens, and a block's rendering depends only on its own source. The only cross-block effect is a link reference definition, and uah falls back to a full render when one is present, as Codex does.

`markdown.Renderer` (in `internal/tui/render/markdown`) keeps, per recently rendered document, the stable prefix of its source (the finished blocks) and their rendered lines:

1. `Render(text, width, first, rest)` looks for a kept document with the same width and prefixes whose stable source is a prefix of `text` (a byte comparison; at most 16 are kept, least recently used first out). A kept document whose last text `text` does not continue is copied, not changed, so two messages that start alike do not overwrite each other's blocks.
2. It parses only `text` after the stable prefix. Every top-level block but the last is finished: it renders once and joins the stable lines, and the stable prefix grows to the start of the last block's line.
3. The last block renders every time. Stable lines and the last block's lines, with one blank line between blocks, are the result.
4. The same text again returns the kept result.

The key of a finished block is therefore its source position in a document with the same stable text, the width, and the prefixes; the theme is the renderer itself, since each `render.Styles` (one per theme) owns one. A width change finds no kept document and renders the whole text once at the new width, then continues incrementally. Rendering from scratch and rendering incrementally produce the same lines by construction; a test checks it for every prefix of several documents.

The streaming lane needs no new call: `markdownLines` keeps its signature and calls `Renderer.Render`. An item whose text grows re-renders its last block only, whatever the item cache does.

Unlike Codex, uah does not hold back the half line: the streaming lane decides what text to pass, and the renderer is exact for whatever it gets. A half-typed `**bold` shows as text until its closing `**` arrives.

## Tables

GFM tables render as Codex draws them, with the width budget from the terminal:

- **Widths.** Each column's natural width is its widest cell (`ansi.StringWidth`, so wide characters and emoji count two cells). The budget is the width less one cell of padding on each side of a cell and two cells between columns. If the natural widths fit, they are used. Otherwise the widest columns shrink first, all to the same cap (a water-fill), down to a floor of 3 cells.
- **Wrapping.** Cells wrap at word boundaries within their column (`ansi.Wrap`), breaking a word only when it is longer than the column; a row is as tall as its tallest cell. Inline styles work in cells.
- **Alignment.** Left, center, and right from the delimiter row.
- **Borders.** Codex's: no vertical rules; the header bold, a `━` rule under it and a `─` rule between body rows, both dim.
- **Fallback.** When the columns do not fit at 3 cells each, or the shrink leaves a column narrower than its longest word and narrower than 10 cells, the grid would be unreadable: the rows render as records, `Header  value` per cell with the headers padded to one width, a dim rule between rows, and the header above an indented value when even that is too narrow. This is Codex's key/value mode with a simpler trigger; horizontal truncation was rejected because it hides data without saying so.

## Highlighting

- chroma, as before. A lexer is looked up once per language name and kept (`lexers.Get` walks the registry; `lexers.Analyse`, which ran every analyser for a fence without a language, is gone). A fence without a language, or with an unknown one, is plain code in the code color, as in Codex.
- Highlighted lines are cached per renderer by language and code text (the whole code string is the map key, so there are no hash collisions), up to 256 blocks, oldest out first. The cache is per theme because the renderer is.
- While a fence is still open, its last line is usually incomplete. The complete lines are highlighted as one unit (a cache hit until the next newline) and the partial line on its own, so a delta inside a code block costs one short line, not the whole block. A closed fence ends with a newline, so it is one unit.
- Codex's limits apply: code over 512 KB or 10,000 lines, or with a line over 4 KiB, is not highlighted.
- Code is never wrapped; the band truncates it at the width, as before.

## The look

Everything the old renderer drew keeps its look: code on the band without fences, `code` in the code color, bold, italic, headings in bold without their `#`, `•` bullets and dim numbers with a hanging indent, quotes behind a dim `│ `, dim rules, and links as the text with the URL dim in parentheses. New: tables, nested lists and quotes as structure (a code block inside a list sits on the band at the list's indent), task lists (`[x]`, `[ ]`), strikethrough, autolinks and bare URLs as the URL, indented code on the band, images as their alt text and URL, and HTML shown as its source text.

Two changes follow Codex: one blank line separates top-level blocks (the old renderer kept the source's blank lines, so a list directly after a paragraph had none), and several blank lines in the source collapse to one. Soft line breaks stay line breaks, as in Codex and before.

## Performance budget and gates

Budgets, on the fixture above at width 100 (Apple M4 Max; CI machines are slower, which is why the gates below do not time):

| Case | Budget |
| --- | --- |
| One render from nothing | under 3 ms (the old renderer took 2.4 ms without tables) |
| The same text again | under 5 µs, one allocation (the copy of the result) |
| Streaming the fixture 12 bytes at a time | under 150 µs per update on average (was 1.16 ms), and no update parses more than its last block and the delta |

Gates:

- **Work bound (a test).** Streaming several documents in 1- to 20-byte deltas, each update parses at most the bytes of the document's last block (from the start of its line) plus the delta, and renders at most two blocks (the one that just finished and the last). The renderer counts both. This is exact and does not depend on the machine.
- **Allocations (a test).** Rendering the same text again allocates once (`testing.AllocsPerRun`).
- **Equality (a test).** Incremental and full rendering give the same lines for every prefix of several documents, at several widths.
- **Benchmarks.** `go test -run '^$' -bench Markdown -benchmem ./internal/tui/render` reports cold, warm, streaming, and resize numbers with the real theme; CI runs them once (`-benchtime 1x`) so they keep compiling and running.

## As built

Package `internal/tui/render/markdown`: `markdown.go` (the `Renderer`, its kept documents, and the block split), `blocks.go` (paragraphs, headings, lists, quotes, code, HTML, and wrapping), `inline.go`, `table.go` (after Codex; see [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md)), and `highlight.go`. `render.Styles` builds one `Renderer` from the theme, and `markdownLines` calls it. The streaming lane's call site does not change.

Numbers from `go test -run '^$' -bench Markdown -benchmem ./internal/tui/render`, same fixture, width 100, Apple M4 Max:

| Case | Before | After |
| --- | --- | --- |
| One render from nothing | 2.39 ms, 15,061 allocations (no tables) | 0.87 ms, 7,351 allocations (with tables) |
| The same text again | 2.39 ms | 0.66 µs, 1 allocation |
| One streaming update, averaged over 543 | 1.16 ms, 6,980 allocations | 30.5 µs, 173 allocations |
| A new width, code already highlighted | 2.39 ms | 0.52 ms |

Two things the build added to the design:

- **Styles that wrap.** A style that wraps onto the next line used to end at the line's first reset, such as the quote bar's, so the second line of a quote lost its dim. Wrapped lines now end their open styles and open them again on the next line (`carry`), in paragraphs, headings, and table cells.
- **Runs.** Text in a row with the same styles is drawn as one run, so goldmark's split text nodes do not each get their own escape sequences.

Tests: goldens for every construct at three widths (the table at four, down to the stacked records) in `internal/tui/render/markdown/testdata`, drawn with styles that emit their own SGR codes and show as `<b>…</>`, so the goldens show styling and the widths are those of real styles; incremental equals full for every prefix of each fixture at two widths, and for the long answer streamed in 1- to 20-byte deltas; each update parses no more than the text after the previous last block; the same text again allocates once; width changes; two texts that share their first blocks; each renderer's styles are its own; highlighting cached per block and an open fence's complete lines cached; lines fit the width.

## Open

Defaults taken:

- **Numbering.** An ordered list numbers from its first number up, as CommonMark and Codex do: `3.`, `4.`, `10.` draws `3.`, `4.`, `5.`.
- **Block spacing.** One blank line between top-level blocks, as Codex; the old renderer kept the source's blank lines. The golden screens did not change; the `markdownLines` test gained four blank lines.
- **A fence without a language** is no longer guessed at, as in Codex. It was highlighted when chroma's analysers recognised it.
- **No half-line holdback.** Codex renders only complete lines while streaming; uah draws whatever text the streaming lane passes. If a half-typed `**bold` flickering to bold is a problem, the streaming lane can pass the text up to the last newline, and the renderer needs no change.
- **Task lists** show `[x]` and `[ ]` after the bullet, dim, rather than check-mark glyphs whose width varies across terminals.

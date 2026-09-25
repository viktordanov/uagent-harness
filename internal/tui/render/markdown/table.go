package markdown

// Tables are laid out as Codex lays them out (codex-rs/tui/src/
// markdown_render.rs, render_table_lines, and markdown_render/
// table_key_value.rs, at rust-v0.156.1; Apache License 2.0, Copyright 2025
// OpenAI): columns padded by one cell and two apart, no vertical rules, a
// "━" rule under the header and "─" rules between rows, widths shrunk to
// fit, and rows drawn as records when the grid would be unreadable. The
// shrinking and the switch to records are simpler than Codex's.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
)

const (
	cellPad   = 1
	columnGap = 2
	// minColumn is the narrowest column.
	minColumn = 3
	// readable is how narrow a column may get below its longest word before
	// the rows are drawn as records.
	readable = 10
	// minValue is the narrowest a record's value may be beside its labels
	// before the labels go above the values.
	minValue = 12
)

// table draws a GFM table at the frame's width.
func (r *Renderer) table(src []byte, t *east.Table, f frame) []string {
	cols := len(t.Alignments)
	if cols == 0 {
		return nil
	}
	var header []string
	var rows [][]string
	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		if _, ok := row.(*east.TableHeader); ok {
			header = r.cells(src, row, cols, append(r.base(f), r.st.TableHeader))
		} else {
			rows = append(rows, r.cells(src, row, cols, r.base(f)))
		}
	}
	if header == nil {
		header = make([]string, cols)
	}
	natural, words := measure(header, rows, cols)
	widths := fit(natural, words, f.w-cols*2*cellPad-(cols-1)*columnGap)
	if widths == nil {
		return r.records(header, rows, f.w)
	}
	out := r.row(header, widths, t.Alignments)
	out = append(out, r.rule("━", widths))
	for i, row := range rows {
		if i > 0 {
			out = append(out, r.rule("─", widths))
		}
		out = append(out, r.row(row, widths, t.Alignments)...)
	}

	return out
}

// cells draws a row's cells, as many as the table has columns.
func (r *Renderer) cells(src []byte, row ast.Node, cols int, styles []Style) []string {
	out := make([]string, 0, cols)
	for c := row.FirstChild(); c != nil && len(out) < cols; c = c.NextSibling() {
		out = append(out, strings.TrimSpace(r.inline(src, c, styles)))
	}
	for len(out) < cols {
		out = append(out, "")
	}

	return out
}

// measure returns each column's widest cell and longest word.
func measure(header []string, rows [][]string, cols int) (natural, words []int) {
	natural, words = make([]int, cols), make([]int, cols)
	for c := range cols {
		natural[c] = minColumn
	}
	for _, row := range append([][]string{header}, rows...) {
		for c, cell := range row {
			natural[c] = max(natural[c], ansi.StringWidth(cell))
			for w := range strings.FieldsSeq(ansi.Strip(cell)) {
				words[c] = max(words[c], ansi.StringWidth(w))
			}
		}
	}

	return natural, words
}

// fit returns column widths within budget: the natural widths when they
// fit, else the widest columns shrunk to one cap (a water-fill). It returns
// nil when the columns cannot fit or one would break its words below a
// readable width.
func fit(natural, words []int, budget int) []int {
	if budget < len(natural)*minColumn {
		return nil
	}
	total := func(limit int) int {
		sum := 0
		for _, n := range natural {
			sum += min(n, limit)
		}

		return sum
	}
	lo, hi := minColumn, 0
	for _, n := range natural {
		hi = max(hi, n)
	}
	if total(hi) <= budget {
		return natural
	}
	for lo < hi { // the largest cap that fits
		mid := (lo + hi + 1) / 2
		if total(mid) <= budget {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	widths := make([]int, len(natural))
	spare := budget - total(lo)
	for i, n := range natural {
		widths[i] = min(n, lo)
		if n > lo && spare > 0 {
			widths[i]++
			spare--
		}
		if widths[i] < words[i] && widths[i] < readable {
			return nil
		}
	}

	return widths
}

// row draws one table row: each cell wrapped in its column and aligned.
func (r *Renderer) row(cells []string, widths []int, align []east.Alignment) []string {
	wrapped := make([][]string, len(cells))
	height := 1
	for c, cell := range cells {
		wrapped[c] = carry(strings.Split(ansi.Wrap(cell, widths[c], ""), "\n"))
		height = max(height, len(wrapped[c]))
	}
	pad := strings.Repeat(" ", cellPad)
	out := make([]string, 0, height)
	for i := range height {
		var b strings.Builder
		for c, lines := range wrapped {
			if c > 0 {
				b.WriteString(strings.Repeat(" ", columnGap))
			}
			line := ""
			if i < len(lines) {
				line = ansi.Truncate(lines[i], widths[c], "")
			}
			b.WriteString(pad + aligned(line, widths[c], align[c]) + pad)
		}
		out = append(out, strings.TrimRight(b.String(), " "))
	}

	return out
}

// aligned pads s to width w by the column's alignment.
func aligned(s string, w int, a east.Alignment) string {
	gap := max(w-ansi.StringWidth(s), 0)
	switch a {
	case east.AlignRight:
		return strings.Repeat(" ", gap) + s
	case east.AlignCenter:
		return strings.Repeat(" ", gap/2) + s + strings.Repeat(" ", gap-gap/2)
	case east.AlignLeft, east.AlignNone:
	}

	return s + strings.Repeat(" ", gap)
}

// rule is a dim rule under each column.
func (r *Renderer) rule(ch string, widths []int) string {
	parts := make([]string, len(widths))
	for i, w := range widths {
		parts[i] = strings.Repeat(ch, w+2*cellPad)
	}

	return r.st.Dim.Render(strings.Join(parts, strings.Repeat(" ", columnGap)))
}

// records draws each row as its cells under one another, each after its
// header, with a dim rule between rows: Codex's key/value form for a table
// too wide for the terminal. When even that is too narrow, each header
// goes above its value.
func (r *Renderer) records(header []string, rows [][]string, w int) []string {
	if len(rows) == 0 {
		return header
	}
	label := 0
	for _, h := range header {
		label = max(label, ansi.StringWidth(h))
	}
	side := label+columnGap+minValue <= w
	var out []string
	for i, row := range rows {
		if i > 0 {
			out = append(out, r.st.Dim.Render(strings.Repeat("─", w)))
		}
		for c, value := range row {
			if !side {
				out = append(out, header[c])
				for _, l := range wrap(value, w-2) {
					out = append(out, strings.TrimRight("  "+l, " "))
				}

				continue
			}
			indent := strings.Repeat(" ", label+columnGap)
			for j, l := range wrap(value, w-label-columnGap) {
				lead := indent
				if j == 0 {
					lead = header[c] + strings.Repeat(" ", label-ansi.StringWidth(header[c])+columnGap)
				}
				out = append(out, strings.TrimRight(lead+l, " "))
			}
		}
	}

	return out
}

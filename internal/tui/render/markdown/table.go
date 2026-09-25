package markdown

// Tables are laid out as Codex lays them out (codex-rs/tui/src/
// markdown_render.rs, render_table_lines, and markdown_render/
// table_key_value.rs, at rust-v0.156.1; Apache License 2.0, Copyright 2025
// OpenAI): columns padded by one cell and two apart, no vertical rules,
// widths shrunk to fit, and rows drawn as records when the grid would be
// unreadable. The shrinking and the switch to records are simpler than
// Codex's. uah draws no rules: the header is in its own style and every
// other row sits on the band (zebra rows).

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
	total := (len(widths) - 1) * columnGap
	for _, w := range widths {
		total += w + 2*cellPad
	}
	out := r.row(header, widths, t.Alignments)
	for i, row := range rows {
		out = append(out, r.zebra(i, r.row(row, widths, t.Alignments), total)...)
	}

	return out
}

// zebra puts row i's lines on the band, w cells wide, when i is even.
func (r *Renderer) zebra(i int, lines []string, w int) []string {
	if i%2 == 0 {
		for j, l := range lines {
			lines[j] = r.st.Band(l, w)
		}
	}

	return lines
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

// records draws each row as its cells under one another, each after its
// header: Codex's key/value form for a table too wide for the terminal,
// with every other record on the band as the grid's rows are. When even
// that is too narrow, each header goes above its value.
func (r *Renderer) records(header []string, rows [][]string, w int) []string {
	if len(rows) == 0 {
		return header
	}
	label := 0
	for _, h := range header {
		label = max(label, ansi.StringWidth(h))
	}
	// A cell of padding on each side, as in the grid.
	inner := w - 2*cellPad
	side := label+columnGap+minValue <= inner
	var out []string
	for i, row := range rows {
		var lines []string
		for c, value := range row {
			if side {
				lines = append(lines, sideBySide(header[c], value, label, inner)...)
			} else {
				lines = append(lines, stacked(header[c], value, inner)...)
			}
		}
		for j, l := range lines {
			lines[j] = strings.TrimRight(strings.Repeat(" ", cellPad)+l, " ")
		}
		out = append(out, r.zebra(i, lines, w)...)
	}

	return out
}

// sideBySide is a record's cell as "Header  value", the header padded to
// label cells and the value wrapped beside it.
func sideBySide(header, value string, label, w int) []string {
	indent := strings.Repeat(" ", label+columnGap)
	var out []string
	for j, l := range wrap(value, w-label-columnGap) {
		lead := indent
		if j == 0 {
			lead = header + strings.Repeat(" ", label-ansi.StringWidth(header)+columnGap)
		}
		out = append(out, lead+l)
	}

	return out
}

// stacked is a record's cell as its header above its value, indented.
func stacked(header, value string, w int) []string {
	out := []string{header}
	for _, l := range wrap(value, w-2) {
		out = append(out, "  "+l)
	}

	return out
}

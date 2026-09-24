package render

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/viktordanov/uagent-harness/internal/contextusage"
)

// The /context grid: gridSide×gridSide cells, each 1% of the window, as
// Claude Code draws it.
const gridSide = 10

// The grid's cells: a filled dot per used percent in its category's color,
// a dim dot for free space, and a ring for the auto-compaction buffer.
const (
	cellUsed   = "●"
	cellFree   = "·"
	cellBuffer = "○"
)

// breakdowns are the categories whose items are listed below the grid.
var breakdowns = []string{contextusage.Instructions, contextusage.Skills, contextusage.MCPTools, contextusage.Tools}

// contextLines draws /context: the grid with the legend beside it, then the
// items of the categories that have named parts.
func (st *Styles) contextLines(u *contextusage.Usage, w int) []string {
	if u == nil || u.Window <= 0 {
		return nil
	}
	grid := st.contextGrid(*u)
	legend := st.contextLegend(*u)
	out := []string{"", st.bold.Render("● Context Usage")}
	for i := range max(len(grid), len(legend)) {
		line := "  "
		if i < len(grid) {
			line += grid[i]
		} else {
			line += strings.Repeat(" ", gridSide*2)
		}
		if i < len(legend) {
			line += "  " + legend[i]
		}
		out = append(out, ansi.Truncate(line, w, "…"))
	}

	return append(out, st.contextItems(*u, w)...)
}

// contextGrid colors one cell per percent: categories in order, then free
// space, then the auto-compaction buffer at the end.
func (st *Styles) contextGrid(u contextusage.Usage) []string {
	total := gridSide * gridSide
	cells := make([]string, 0, total)
	for _, c := range u.Categories {
		n := int((c.Tokens*int64(total) + u.Window/2) / u.Window)
		if n == 0 && c.Tokens > 0 {
			n = 1
		}
		for range n {
			if len(cells) < total {
				cells = append(cells, st.categoryColors[c.Name].Render(cellUsed))
			}
		}
	}
	buffer := int((u.Buffer*int64(total) + u.Window/2) / u.Window)
	for len(cells) < total-buffer {
		cells = append(cells, st.dim.Render(cellFree))
	}
	for len(cells) < total {
		cells = append(cells, st.dim.Render(cellBuffer))
	}
	rows := make([]string, 0, gridSide)
	for r := range gridSide {
		rows = append(rows, strings.Join(cells[r*gridSide:(r+1)*gridSide], " ")+" ")
	}

	return rows
}

func (st *Styles) contextLegend(u contextusage.Usage) []string {
	est := ""
	if u.Estimated {
		est = " (estimated)"
	}
	lines := []string{
		fmt.Sprintf("%s · %s/%s tokens (%s)%s", modelLabelShort(u.Model), tokens(u.Used), tokens(u.Window), pct(u.Used, u.Window), est),
		"",
	}
	for _, c := range u.Categories {
		lines = append(lines, fmt.Sprintf("%s %s: %s tokens (%s)", st.categoryColors[c.Name].Render(cellUsed), c.Name, tokens(c.Tokens), pct(c.Tokens, u.Window)))
	}
	lines = append(lines, fmt.Sprintf("%s Free space: %s tokens (%s)", st.dim.Render(cellFree), tokens(u.Free()), pct(u.Free(), u.Window)))
	if u.Buffer > 0 {
		lines = append(lines, fmt.Sprintf("%s Auto-compact buffer: %s tokens (%s)", st.dim.Render(cellBuffer), tokens(u.Buffer), pct(u.Buffer, u.Window)))
	}

	return lines
}

// contextItems lists each named part of the categories that have them.
func (st *Styles) contextItems(u contextusage.Usage, w int) []string {
	var out []string
	for _, name := range breakdowns {
		for _, c := range u.Categories {
			if c.Name != name || len(c.Items) == 0 {
				continue
			}
			out = append(out, "", "  "+st.bold.Render(c.Name))
			for _, it := range c.Items {
				line := fmt.Sprintf("  └ %s: %s tokens", it.Name, tokens(it.Tokens))
				out = append(out, st.dim.Render(ansi.Truncate(line, w, "…")))
			}
		}
	}

	return out
}

func pct(n, of int64) string {
	if of <= 0 {
		return "0%"
	}
	p := float64(n) * 100 / float64(of)
	if p > 0 && p < 1 {
		return fmt.Sprintf("%.1f%%", p)
	}

	return fmt.Sprintf("%.0f%%", p)
}

func modelLabelShort(m string) string {
	if m == "" {
		return "model"
	}

	return m
}

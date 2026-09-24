package render

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"

	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// configLines draw the /config panel above the composer, after Claude
// Code's settings list: a row per setting with its value and where the
// value comes from, the selected row in the accent band, and the keys.
func (st *Styles) configLines(s state.State, w int) []string {
	p := s.Config
	title := st.accent.Render(" Settings")
	if p.Path != "" {
		title += st.dim.Render(" · saved to " + home(p.Path))
	}
	out := []string{ansi.Truncate(title, w, "…")}
	if p.Values == nil {
		return append(out, st.dim.Render("   loading…"))
	}
	for i, row := range s.ConfigRows() {
		value := row.Value
		switch {
		case i == p.Index && p.Editing:
			value = p.Input + "▏"
		case row.Toggle() && value == "on":
			value = st.ok.Render(value)
		}
		label := fmt.Sprintf("%-26s", row.Label)
		line := "   " + label + " " + padTo(value, 30) + " " + st.dim.Render(row.Source)
		if i == p.Index {
			line = st.selected.Render(" › "+label) + " " + st.bold.Render(padTo(value, 30)) + " " + st.dim.Render(row.Source)
		}
		out = append(out, ansi.Truncate(line, w, "…"))
	}
	hint := "   ↑↓ choose · enter or space change · ←→ cycle · esc close"
	if p.Editing {
		hint = "   type a value · enter save · esc cancel"
	}

	return append(out, st.dim.Render(ansi.Truncate(hint, w, "…")))
}

// padTo pads styled text to n columns.
func padTo(text string, n int) string {
	if gap := n - ansi.StringWidth(text); gap > 0 {
		return text + fmt.Sprintf("%*s", gap, "")
	}

	return text
}

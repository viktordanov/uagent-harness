package render

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"

	"github.com/viktordanov/uah/internal/tui/state"
)

// pickerRows is how many rows the /model picker shows at once.
const pickerRows = 8

// modelPickerLines draw the /model picker above the composer, after
// Codex's model and reasoning popups: a title, a row per model (or per
// effort of the chosen model) with the selected one in the accent band,
// and the keys.
func (st *Styles) modelPickerLines(s state.State, w int) []string {
	p := s.ModelPicker
	if p.Loading {
		return []string{
			ansi.Truncate(st.accent.Render(" Select model")+st.dim.Render(" · "+s.Settings.Provider), w, "…"),
			st.dim.Render("   loading models…"),
		}
	}
	var title, hint string
	var labels, helps []string
	if p.Model == "" {
		title, hint = " Select model"+st.dim.Render(" · "+s.Settings.Provider), "   ↑↓ choose · enter choose its effort · esc close"
		for _, m := range s.PickerModels() {
			label := m.ID
			if m.ID == s.Settings.Model {
				label += " (current)"
			}
			labels, helps = append(labels, label), append(helps, state.ModelHelp(m))
		}
	} else {
		title, hint = " Select effort for "+p.Model, "   ↑↓ choose · enter apply to this session · esc back to models"
		for _, r := range s.PickerEfforts() {
			label := r.Level
			if r.Default {
				label += " (default)"
			}
			if r.Current {
				label += " (current)"
			}
			labels, helps = append(labels, label), append(helps, r.Help)
		}
	}
	out := []string{ansi.Truncate(st.accent.Render(title), w, "…")}
	width := 0
	for _, l := range labels {
		width = max(width, ansi.StringWidth(l))
	}
	top := min(max(p.Index-pickerRows+1, 0), max(len(labels)-pickerRows, 0))
	for i := top; i < min(top+pickerRows, len(labels)); i++ {
		label := fmt.Sprintf("%-*s", width, labels[i])
		line := "   " + label + "  " + st.dim.Render(helps[i])
		if i == p.Index {
			line = st.selected.Render(" › "+label) + "  " + st.dim.Render(helps[i])
		}
		out = append(out, ansi.Truncate(line, w, "…"))
	}

	return append(out, st.dim.Render(ansi.Truncate(hint, w, "…")))
}

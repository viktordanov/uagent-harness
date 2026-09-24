package render

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// approvalLines is the approval overlay above the composer, after Codex's:
// the question, the model's reason, the command, and the choices with
// their keys.
func (st *Styles) approvalLines(a state.Approval, w int) []string {
	title := "Run this command?"
	if a.Escalation {
		title = "Run outside the sandbox?"
	}
	out := []string{st.warn.Bold(true).Render(ansi.Truncate(" "+title, w, "…"))}
	if a.Justification != "" {
		out = append(out, st.italic.Render(ansi.Truncate(" Reason: "+oneLine(a.Justification), w, "…")))
	}
	for line := range strings.SplitSeq(strings.TrimRight(a.Command, "\n"), "\n") {
		out = append(out, st.bold.Render(ansi.Truncate(" $ "+line, w, "…")))
		if len(out) > 8 {
			out = append(out, st.dim.Render(" …"))

			break
		}
	}
	if a.Answered {
		return append(out, st.dim.Render(" answering…"))
	}
	choices := [][2]string{{"y", "Yes, proceed"}}
	if len(a.Prefix) > 0 {
		choices = append(choices, [2]string{"s", "Yes, and don't ask again for commands that start with `" + strings.Join(a.Prefix, " ") + "`"})
	}
	choices = append(choices, [2]string{"n", "No, and tell the agent what to do differently (esc)"})
	for _, c := range choices {
		out = append(out, ansi.Truncate("   "+st.bold.Render(c[0])+"  "+c[1], w, "…"))
	}

	return out
}

package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// Output rows a command the user ran shows: its first and last rows around
// a fold, fewer in the compact view, as Codex shows up to 50 rows of a
// user shell command.
const (
	shellRowsCompact = 10
	shellRowsDetails = 50
)

// ShellPrompt is the composer's mark before its first row: ! in shell mode,
// λ otherwise. The composer draws it in the accent.
func ShellPrompt(s state.State) string {
	if s.Shell {
		return "! "
	}

	return "λ "
}

// ShellPlaceholder is the empty composer's hint.
func ShellPlaceholder(s state.State) string {
	if s.Shell {
		return "Run a command in the workspace · esc to leave shell mode"
	}

	return "Ask uah to do anything · / for commands"
}

// shellLines draws a command the user ran: the command on the band after
// an accent !, how it ended, and its output folded, as Codex's "You ran"
// cell.
func (st *Styles) shellLines(it state.Item, w int, now time.Time, details bool) []string {
	out := st.bandLines("! ", it.Text, "  "+st.shellStatus(it, now), w)
	if it.Label != "" {
		return append(out, styleLines(wrapPrefixed(it.Label, w, "  ", "  "), st.bad)...)
	}
	rows := shellRowsCompact
	if details {
		rows = shellRowsDetails
	}
	var body []string
	for line := range strings.SplitSeq(strings.TrimRight(it.Detail, "\n"), "\n") {
		body = append(body, wrapPrefixed(line, w, "  ", "  ")...)
	}
	switch {
	case strings.TrimSpace(it.Detail) == "" && it.Tool != state.ToolRunning:
		body = []string{"  (no output)"}
	case it.Tool == state.ToolRunning && len(body) > rows:
		body = body[len(body)-rows:] // the latest output while it runs
	case len(body) > rows:
		fold := fmt.Sprintf("  … %d more lines", len(body)-rows)
		body = append(append(body[:rows/2:rows/2], fold), body[len(body)-rows+rows/2:]...)
	}
	out = append(out, styleLines(body, st.dim)...)
	if it.Tool != state.ToolRunning && it.Input != state.InputDelivered {
		out = append(out, st.dim.Render("  · the agent sees this with your next message"))
	}

	return out
}

// shellStatus is how the command ended, or how long it has run.
func (st *Styles) shellStatus(it state.Item, now time.Time) string {
	switch it.Tool {
	case state.ToolRunning, state.ToolCalled:
		return st.tool.Render(spin(now)) + st.dim.Render(" running "+clock(now.Sub(it.Started)))
	case state.ToolOK:
		return st.ok.Render("✓") + st.dim.Render(" · "+secs(it.Duration))
	case state.ToolStopped:
		return st.warn.Render("■ stopped") + st.dim.Render(" · "+secs(it.Duration))
	case state.ToolFailed:
	}
	if it.Label != "" {
		return st.bad.Render("✗ not run")
	}

	return st.bad.Render(fmt.Sprintf("✗ exit %d", it.Exit)) + st.dim.Render(" · "+secs(it.Duration))
}

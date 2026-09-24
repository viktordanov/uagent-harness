package render

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// The compact view, Codex-shaped: your messages on a band after a λ, tool
// calls as a dim labeled column, "•" before the agent's messages, and the
// time and duration under a finished run.

// labelWidth is each of the tool column's two fields: a label ("RAN") and
// its time or state, at most six characters and a space.
const labelWidth = 7

// compactLines draws an item in the compact view. ok is false for kinds
// drawn the same in both views.
func (st *Styles) compactLines(it state.Item, w int, now time.Time) ([]string, bool) {
	switch it.Kind {
	case state.KindRun, state.KindTurn:
		return nil, true
	case state.KindFinish:
		return []string{"", st.finishLine(it)}, true
	case state.KindTool:
		if len(it.Diff) > 0 {
			return st.patchLines(it, w, now, compactDiffLines), true
		}

		return []string{st.compactTool(it, w, now)}, true
	case state.KindAgent:
		// A running subagent is drawn at the bottom, above the working line.
		if it.Detail == engine.AgentRunning {
			return nil, true
		}

		return st.agentLines(it, w, now), true
	case state.KindAssistant:
		mark := st.accent.Render("• ")
		if it.Final {
			return append([]string{""}, st.markdownLines(it.Text, w, mark, "  ")...), true
		}

		return st.markdownLines(it.Text, w, mark, "  "), true
	case state.KindNotice:
		if it.Level == state.LevelDebug {
			return nil, true
		}
	case state.KindUser, state.KindReasoning:
	}

	return nil, false
}

// userLines draws your message on the band, after a λ, with a band row
// above and below as Codex draws it.
func (st *Styles) userLines(text, suffix string, w int) []string {
	return st.bandLines("λ ", text, suffix, w)
}

// bandLines draws text on the band after an accent mark: λ for a message,
// ! for a command the user ran.
func (st *Styles) bandLines(mark, text, suffix string, w int) []string {
	lines := wrapPrefixed(text, w, st.accent.Render(mark), "  ")
	lines[len(lines)-1] += suffix
	out := []string{"", st.band("", w)}
	for _, l := range lines {
		out = append(out, st.band(l, w))
	}

	return append(out, st.band("", w))
}

// finishLine ends a run: "12:14 PM · worked 1m 12s", Codex's time and
// Claude Code's duration on one line, after how it ended when not ok.
func (st *Styles) finishLine(it state.Item) string {
	when := st.dim.Render(fmt.Sprintf("%s · worked %s", it.Started.Add(it.Wall).Local().Format("3:04 PM"), elapsed(it.Wall)))
	switch it.Status {
	case core.StatusOK, core.StatusRunning:
		return "  " + when
	case core.StatusInterrupted:
		return st.warn.Render("  ■ interrupted") + st.dim.Render(" · ") + when
	case core.StatusTimeout, core.StatusDiskLimit, core.StatusFailed:
	}

	return st.bad.Render(fmt.Sprintf("  ✗ run ended: %s", it.Status)) + st.dim.Render(" · ") + when
}

// compactTool draws a tool call as a column: "  RAN   4.1s  go test ./...".
// A live call is in the accent; a finished one is dim.
func (st *Styles) compactTool(it state.Item, w int, now time.Time) string {
	label, live := toolLabel(it.Name), false
	var when string
	switch it.Tool {
	case state.ToolCalled, state.ToolRunning:
		live, when = true, elapsed(now.Sub(it.Started))
		if label == "RAN" {
			label = "RUN"
		}
	case state.ToolOK:
		if it.Duration >= time.Second {
			when = secs(it.Duration)
		}
	case state.ToolFailed:
		when = st.bad.Render(pad("fail")) + " "
	case state.ToolStopped:
		when = st.warn.Render(pad("stop")) + " "
	}
	var tail string
	if it.Tool == state.ToolFailed && it.Detail != "" {
		tail = st.bad.Render("  " + oneLine(it.Detail))
	}
	var head string
	switch {
	case live:
		head = st.accent.Render("  " + pad(label) + pad(when) + " ")
	case it.Tool == state.ToolFailed || it.Tool == state.ToolStopped:
		head = st.dim.Render("  "+pad(label)) + when
	default:
		head = st.dim.Render("  " + pad(label) + pad(when) + " ")
	}
	room := max(w-ansi.StringWidth(head)-ansi.StringWidth(tail), 8)
	text := ansi.Truncate(oneLine(untab(it.Label)), room, "…")
	if live {
		return head + text
	}

	return head + st.dim.Render(text) + tail
}

// toolLabels name the tools whose label is not their name's first word in
// capitals.
var toolLabels = map[string]string{
	"Bash":        "RAN",
	"spawn_agent": "SPAWN", "send_input": "SEND", "wait_agent": "WAIT", "wait": "WAIT",
	"close_agent": "CLOSE", "resume_agent": "RESUME",
	"apply_patch": "EDIT",
}

// toolLabel is a tool's column label: RAN for commands, MCP for a server's
// tools, the agent tools' verbs, else the first word of the name in
// capitals ("SkillUse" is SKILL, "view_image" is VIEW).
func toolLabel(name string) string {
	if l, ok := toolLabels[name]; ok {
		return l
	}
	if strings.HasPrefix(name, "mcp__") {
		return "MCP"
	}
	name, _, _ = strings.Cut(name, "_")
	for i, r := range name {
		if i > 0 && unicode.IsUpper(r) {
			name = name[:i]

			break
		}
	}
	name = strings.ToUpper(name)

	return name[:min(len(name), labelWidth-1)]
}

// untab expands tabs to four spaces: width math counts a tab as nothing,
// while the terminal moves to its next stop, so a tabbed line would spill
// past its band or its width.
func untab(s string) string { return strings.ReplaceAll(s, "\t", "    ") }

// agentLines draws a subagent as a tree: "  AGENT Ada  0:42", and under a
// running one what it is doing now.
func (st *Styles) agentLines(it state.Item, w int, now time.Time) []string {
	name := it.Name
	if it.Label != "" {
		name += st.dim.Render(" " + it.Label)
	}
	var head string
	switch it.Detail {
	case engine.AgentRunning:
		head = st.accent.Render("  AGENT ") + name + st.dim.Render("  "+elapsed(now.Sub(it.Started)))
	case engine.AgentCompleted:
		done := "  done"
		if it.Duration > 0 {
			done += " in " + elapsed(it.Duration)
		}
		head = st.dim.Render("  AGENT ") + name + st.dim.Render(done)
	case engine.AgentErrored:
		why := "  failed"
		if it.Agent != nil && it.Agent.Message != "" {
			why += ": " + oneLine(it.Agent.Message)
		}
		head = st.dim.Render("  AGENT ") + name + st.bad.Render(why)
	default:
		head = st.dim.Render("  AGENT ") + name + st.dim.Render("  "+it.Detail)
	}
	out := []string{ansi.Truncate(head, w, "…")}
	if it.Detail == engine.AgentRunning {
		out = append(out, ansi.Truncate(st.dim.Render("    └ ")+st.tool.Render(spin(now))+st.dim.Render(" "+agentDoing(it)), w, "…"))
	}

	return out
}

// agentDoing is what a running subagent does now: its latest live tool
// call ("Read internal/tui/…"), else "thinking".
func agentDoing(it state.Item) string {
	for _, sub := range slices.Backward(it.Sub) {
		if sub.Tool == state.ToolCalled || sub.Tool == state.ToolRunning {
			verb := sub.Name
			if verb == "Bash" {
				verb = "Running"
			}

			return verb + " " + oneLine(sub.Label)
		}
	}

	return "thinking"
}

// activeAgents draws the running subagents as trees for the bottom of the
// screen, with a blank line above each.
func (st *Styles) activeAgents(s state.State, w int) []string {
	var out []string
	for _, it := range s.Agents() {
		if state.Working(it) {
			out = append(out, "")
			out = append(out, st.agentLines(it, w, s.Now)...)
		}
	}

	return out
}

// banner is Codex's box at the top of the transcript.
func (st *Styles) banner(s state.State, version string, w int) []string {
	if version != "" {
		version = " (" + version + ")"
	}
	rows := []string{
		st.accent.Render("λ uah") + st.dim.Render(version),
		"",
		st.dim.Render("model:     ") + strings.TrimSpace(s.Settings.Model+" "+s.Settings.Effort) + st.dim.Render("   /model to change"),
		st.dim.Render("directory: ") + home(s.Settings.Workspace),
	}
	inner := 0
	for _, r := range rows {
		inner = max(inner, ansi.StringWidth(r))
	}
	inner = min(inner+2, max(w-4, 10))
	out := []string{st.dim.Render("╭" + strings.Repeat("─", inner+2) + "╮")}
	for _, r := range rows {
		r = ansi.Truncate(r, inner, "…")
		out = append(out, st.dim.Render("│ ")+r+strings.Repeat(" ", inner-ansi.StringWidth(r))+st.dim.Render(" │"))
	}

	return append(out, st.dim.Render("╰"+strings.Repeat("─", inner+2)+"╯"), "")
}

// workingLine is the breathing λ and "Working (12s • esc to interrupt)";
// without a run (since is zero) only the λ and the verb.
func (st *Styles) workingLine(now time.Time, verb string, since time.Time) string {
	line := st.breathing(now.UnixMilli()) + " " + st.bold.Render(verb)
	if since.IsZero() {
		return line
	}

	return line + st.dim.Render(fmt.Sprintf(" (%s • esc to interrupt)", elapsed(now.Sub(since))))
}

// elapsed is Codex's short duration: 12s, 1m 12s, 1h 02m.
func elapsed(d time.Duration) string {
	d = max(d, 0).Round(time.Second)
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	}

	return fmt.Sprintf("%ds", int(d.Seconds()))
}

// pad fills s to the tool column's width.
func pad(s string) string {
	return s + strings.Repeat(" ", max(labelWidth-ansi.StringWidth(s), 0))
}

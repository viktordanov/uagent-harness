package render

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/images"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// Frame is what the shell provides for one screen.
type Frame struct {
	Width, Height int
	// Composer is the text input's rendered view and ComposerHeight its lines.
	Composer       string
	ComposerHeight int
	// Draft is the composer's text, for command completion.
	Draft string
	// Version is uah's version, for the banner.
	Version string
}

// Screen draws the whole screen and returns the row where the composer starts.
func Screen(s state.State, c *Cache, f Frame) (string, int) {
	if f.Width <= 0 || f.Height <= 0 {
		return "", 0
	}
	if len(s.Items) == 0 && len(c.entries) > 0 {
		c.entries = map[string]cacheEntry{} // /clear, /new, or a reload: the old lines go
	}
	if s.Mode == state.ModePicker {
		return c.styles.picker(s, f), -1
	}
	if s.View != nil && len(s.Approvals) == 0 { // an approval is the session's: it shows there
		return agentScreen(s, c, f)
	}
	var top []string
	if s.Details {
		top = append(top, c.styles.headerLine(s, f.Width))
	}
	panel := c.styles.panelLines(s, f)
	bottom := make([]string, 0, len(panel)+f.ComposerHeight+3)
	if !s.Details {
		bottom = append(bottom, c.styles.activeAgents(s, f.Width)...)
		if line := c.styles.statusLine(s, f.Width); line != "" {
			bottom = append(bottom, "", line)
		}
	}
	bottom = append(bottom, panel...)
	// The composer sits on the band, with a band row above and below.
	bottom = append(bottom, "", c.styles.band("", f.Width))
	composerTop := len(bottom)
	for l := range strings.SplitSeq(f.Composer, "\n") {
		bottom = append(bottom, c.styles.band(l, f.Width))
	}
	bottom = append(bottom, c.styles.band("", f.Width), c.styles.footerLine(s, f.Width))

	height := max(f.Height-len(top)-len(bottom), 1)
	var head []string
	if !s.Details && s.SessionID != "" {
		head = c.styles.banner(s, f.Version, f.Width)
	}
	body := transcript(s, c, f.Width, height, head)
	lines := make([]string, 0, f.Height)
	lines = append(lines, top...)
	lines = append(lines, body...)
	composerRow := len(lines) + composerTop
	lines = append(lines, bottom...)
	if excess := len(lines) - f.Height; excess > 0 {
		lines = lines[excess:]
		composerRow -= excess
	}

	return strings.Join(lines, "\n"), composerRow
}

// transcript returns exactly height lines ending at the scroll position,
// rendering items from the bottom up and stopping once the window is full.
// head, when the whole transcript fits above the scroll position, comes
// first: the banner.
func transcript(s state.State, c *Cache, w, height int, head []string) []string {
	need := height + s.Scroll
	var rev [][]string
	count := 0
	i := len(s.Items) - 1
	for ; i >= 0 && count < need; i-- {
		lines := c.lines(s.Items[i], w, s.Now, view{reasoning: s.ShowReasoning, details: s.Details})
		if len(lines) == 0 {
			continue
		}
		rev = append(rev, lines)
		count += len(lines)
	}
	all := make([]string, 0, count)
	for _, lines := range slices.Backward(rev) {
		all = append(all, lines...)
	}
	c.maxScroll = -1
	if i < 0 {
		all = append(slices.Clip(head), all...)
		c.maxScroll = max(len(all)-height, 0)
	}
	end := len(all) - min(s.Scroll, max(len(all)-height, 0))
	start := max(end-height, 0)
	window := all[start:end]
	out := make([]string, 0, height)
	for range height - len(window) {
		out = append(out, "")
	}
	for _, l := range window {
		out = append(out, ansi.Truncate(l, w, ""))
	}

	return out
}

func (st *Styles) headerLine(s state.State, w int) string {
	left := fmt.Sprintf(" uah · %s · %s/%s · %s · %s · %s", session.ShortID(s.SessionID), s.Settings.Provider, s.Settings.Model, s.Settings.Effort, cmp.Or(modeText(s), "sandbox none"), home(s.Settings.Workspace))
	var right string
	switch {
	case s.SessionID == "":
		right = "opening… "
	case s.Live != nil:
		right = fmt.Sprintf("● running %s ", clock(s.Now.Sub(s.Live.Started)))
	case s.Busy:
		right = "● starting "
	case s.Quitting:
		right = "stopping "
	default:
		right = "idle "
	}
	gap := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		left = ansi.Truncate(left, max(w-ansi.StringWidth(right)-1, 0), "…")
		gap = max(w-ansi.StringWidth(left)-ansi.StringWidth(right), 0)
	}

	return st.header.Render(left + strings.Repeat(" ", gap) + right)
}

// panelLines shows a pending approval, the /config panel, the suggestion
// menu while typing a command or an "@" mention, or else the queue.
func (st *Styles) panelLines(s state.State, f Frame) []string {
	if a, ok := s.PendingApproval(); ok {
		return st.approvalLines(a, f.Width)
	}
	if s.Config != nil {
		return st.configLines(s, f.Width)
	}
	if items := s.Suggestions(f.Draft); len(items) > 0 {
		var out []string
		for i, it := range items[:min(len(items), 6)] {
			label := fmt.Sprintf("%-16s", it.Label)
			line := "  " + st.bold.Render(label) + "  " + st.dim.Render(it.Help)
			if i == s.Menu.Index {
				line = st.selected.Render("› "+label) + "  " + st.dim.Render(it.Help)
			}
			out = append(out, ansi.Truncate(line, f.Width, "…"))
		}

		return out
	}
	if len(s.Queue) == 0 {
		return nil
	}
	var out []string
	if s.Details {
		out = append(out, st.dim.Render(ansi.Truncate("queued · sent when the agent is ready · ctrl+enter sends now · ↑ edits the last", f.Width, "…")))
	}
	for i, q := range s.Queue {
		if i == 3 {
			out = append(out, st.dim.Render(fmt.Sprintf("  … %d more", len(s.Queue)-3)))

			break
		}
		if s.Details {
			out = append(out, ansi.Truncate(fmt.Sprintf("  %d. %s", i+1, oneLine(images.Display(q.Text))), f.Width, "…"))
		} else {
			out = append(out, st.dim.Render(ansi.Truncate("  ↳ queued: ", f.Width, ""))+ansi.Truncate(oneLine(images.Display(q.Text)), max(f.Width-12, 8), "…"))
		}
	}
	if !s.Details {
		out = append(out, st.dim.Render("    ctrl+enter sends now · ↑ edits"))
	}

	return out
}

// statusLine is the compact view's activity line above the composer: the
// breathing λ and what the agent does, as Codex's "Working (12s • esc to
// interrupt)".
func (st *Styles) statusLine(s state.State, w int) string {
	switch {
	case s.Status != "":
		return st.warn.Render(ansi.Truncate(s.Status, w, "…"))
	case s.SessionID == "":
		return ansi.Truncate(st.workingLine(s.Now, "Opening the session", time.Time{}), w, "…")
	case s.Live != nil && !s.Live.TurnSince.IsZero():
		return ansi.Truncate(st.workingLine(s.Now, "Thinking", s.Live.Started), w, "…")
	case s.Live != nil && s.Live.Tools > 0:
		return ansi.Truncate(st.workingLine(s.Now, "Running "+plural(s.Live.Tools, "command"), s.Live.Started), w, "…")
	case s.Live != nil:
		return ansi.Truncate(st.workingLine(s.Now, "Working", s.Live.Started), w, "…")
	case s.Busy:
		return ansi.Truncate(st.workingLine(s.Now, "Starting", time.Time{}), w, "…")
	}

	return ""
}

func (st *Styles) footerLine(s state.State, w int) string {
	if !s.Details {
		var parts []string
		fast := ""
		if s.Settings.ServiceTier != "" {
			fast = "fast"
		}
		for _, p := range []string{strings.TrimSpace(s.Settings.Model + " " + s.Settings.Effort), fast, modeText(s), home(s.Settings.Workspace)} {
			if p != "" {
				parts = append(parts, p)
			}
		}
		if len(s.Queue) > 0 {
			parts = append(parts, plural(len(s.Queue), "queued message"))
		}
		left := " " + strings.Join(parts, " · ")
		hint := "ctrl+t details · / commands "
		if s.Scroll > 0 {
			hint = "scrolled up · end returns "
		}
		if pct, ok := s.ContextLeft(); ok {
			hint = fmt.Sprintf("%d%% context left · %s", pct, hint)
		}
		// The hint wins over the left side, which is cut when the line is full.
		room := w - ansi.StringWidth(hint)
		if room > 0 {
			left = ansi.Truncate(left, room-1, "…")
			left += strings.Repeat(" ", room-ansi.StringWidth(left)) + hint
		}

		return st.dim.Render(ansi.Truncate(left, w, ""))
	}
	if s.Status != "" {
		return st.warn.Render(ansi.Truncate(" "+s.Status, w, "…"))
	}
	t := s.Totals
	text := fmt.Sprintf(" %s in (%s cached) · %s out · %s · %s (∥%d)", tokens(t.Tokens.InputTokens), tokens(t.Tokens.CachedInputTokens),
		tokens(t.Tokens.OutputTokens), plural(t.Runs, "run"), plural(t.ToolCalls, "tool"), t.MaxParallel)
	if t.ToolBusy > 0 {
		text += fmt.Sprintf(" · overlap %d%%", int(100*t.Overlap/t.ToolBusy))
	}
	hint := "enter send · ctrl+enter now · / commands "
	if left, ok := s.ContextLeft(); ok {
		text += fmt.Sprintf(" · %d%% context left", left)
		hint = "/ commands "
	}
	if gap := w - ansi.StringWidth(text) - ansi.StringWidth(hint); gap > 0 {
		text += strings.Repeat(" ", gap) + hint
	}

	return st.dim.Render(ansi.Truncate(text, w, ""))
}

// modeText is the permission mode, which shift+tab changes, as the footer
// and the detailed header show it ("" without one).
func modeText(s state.State) string {
	m := s.Settings.Mode
	if m == "" && s.Settings.Sandbox != "" {
		m = approval.ModeFor(sandbox.Mode(s.Settings.Sandbox))
	}
	if m == "" {
		return ""
	}

	return m.Label() + " mode"
}

func (st *Styles) picker(s state.State, f Frame) string {
	scope := "this directory · tab: all"
	if s.Picker.All {
		scope = "all directories · tab: this directory"
	}
	lines := []string{st.header.Render(ansi.Truncate(fmt.Sprintf(" Resume a session · %s · filter: %s▏", scope, s.Picker.Filter)+strings.Repeat(" ", f.Width), f.Width, ""))}
	list := s.Picker.Filtered()
	if len(list) == 0 {
		empty := "  no sessions match"
		if !s.Picker.All && s.Picker.Filter == "" {
			empty = "  no sessions in this directory · tab shows all · esc starts a new one"
		}
		lines = append(lines, "", st.dim.Render(empty))
	}
	room := f.Height - 3
	start := max(0, min(s.Picker.Selected-room/2, len(list)-room))
	for i := start; i < len(list) && i < start+room; i++ {
		in := list[i]
		row := fmt.Sprintf(" %s  %-9s %7s  %-11s %-12s ", session.ShortID(in.ID), age(s.Now, in.LastActivity), plural(in.Runs, "run"), in.Status, in.Model)
		if s.Picker.All {
			row += fmt.Sprintf("%-24s ", ansi.Truncate(home(in.Workspace), 24, "…"))
		}
		row += oneLine(in.FirstPrompt)
		row = ansi.Truncate(row, f.Width, "…")
		if i == s.Picker.Selected {
			row = st.selected.Render(row + strings.Repeat(" ", max(f.Width-ansi.StringWidth(row), 0)))
		}
		lines = append(lines, row)
	}
	for len(lines) < f.Height-1 {
		lines = append(lines, "")
	}
	lines = append(lines, st.dim.Render(" type to filter · ↑↓ choose · enter resume · tab this directory/all · esc back"))

	return strings.Join(lines, "\n")
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}

	return fmt.Sprintf("%d %ss", n, noun)
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func home(path string) string {
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, h) {
		return "~" + strings.TrimPrefix(path, h)
	}

	return path
}

func age(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case t.IsZero():
		return "-"
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}

	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

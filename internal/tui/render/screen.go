package render

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

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
}

// Screen draws the whole screen and returns the row where the composer starts.
func Screen(s state.State, c *Cache, f Frame) (string, int) {
	if f.Width <= 0 || f.Height <= 0 {
		return "", 0
	}
	if s.Mode == state.ModePicker {
		return picker(s, f), -1
	}
	var top []string
	if s.Details {
		top = append(top, headerLine(s, f.Width))
	}
	panel := panelLines(s, f)
	bottom := make([]string, 0, len(panel)+f.ComposerHeight+3)
	if !s.Details {
		if line := statusLine(s, f.Width); line != "" {
			bottom = append(bottom, line)
		}
	}
	bottom = append(bottom, panel...)
	bottom = append(bottom, dim.Render(strings.Repeat("─", f.Width)))
	composerTop := len(bottom)
	bottom = append(bottom, strings.Split(f.Composer, "\n")...)
	bottom = append(bottom, footerLine(s, f.Width))

	height := max(f.Height-len(top)-len(bottom), 1)
	body := transcript(s, c, f.Width, height)
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
func transcript(s state.State, c *Cache, w, height int) []string {
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

func headerLine(s state.State, w int) string {
	left := fmt.Sprintf(" uah · %s · %s/%s · %s · %s", short(s.SessionID), s.Settings.Provider, s.Settings.Model, s.Settings.Effort, home(s.Settings.Workspace))
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

	return header.Render(left + strings.Repeat(" ", gap) + right)
}

// panelLines shows command completion while typing a command, else the queue.
func panelLines(s state.State, f Frame) []string {
	if strings.HasPrefix(f.Draft, "/") && !strings.Contains(f.Draft, " ") {
		matches := state.Complete(strings.TrimPrefix(f.Draft, "/"))
		var out []string
		for i, c := range matches {
			if i == 5 {
				break
			}
			name := "/" + c.Name
			if c.Args != "" {
				name += " " + c.Args
			}
			out = append(out, ansi.Truncate(fmt.Sprintf("  %s  %s", bold.Render(fmt.Sprintf("%-16s", name)), dim.Render(c.Help)), f.Width, "…"))
		}

		return out
	}
	if len(s.Queue) == 0 {
		return nil
	}
	var out []string
	if s.Details {
		out = append(out, dim.Render(ansi.Truncate("queued · sent when the agent is ready · ctrl+enter sends now · ↑ edits the last", f.Width, "…")))
	}
	for i, q := range s.Queue {
		if i == 3 {
			out = append(out, dim.Render(fmt.Sprintf("  … %d more", len(s.Queue)-3)))

			break
		}
		if s.Details {
			out = append(out, ansi.Truncate(fmt.Sprintf("  %d. %s", i+1, oneLine(q.Text)), f.Width, "…"))
		} else {
			out = append(out, dim.Render(ansi.Truncate("  ↳ queued: ", f.Width, ""))+ansi.Truncate(oneLine(q.Text), max(f.Width-12, 8), "…"))
		}
	}
	if !s.Details {
		out = append(out, dim.Render("    ctrl+enter sends now · ↑ edits"))
	}

	return out
}

// statusLine is the compact view's activity line above the composer.
func statusLine(s state.State, w int) string {
	var text string
	switch {
	case s.Status != "":
		return warn.Render(ansi.Truncate(s.Status, w, "…"))
	case s.SessionID == "":
		text = spin(s.Now) + " Opening the session"
	case s.Live != nil && !s.Live.TurnSince.IsZero():
		text = fmt.Sprintf("%s Thinking %s · esc to interrupt", spin(s.Now), clock(s.Now.Sub(s.Live.Started)))
	case s.Live != nil && s.Live.Tools > 0:
		text = fmt.Sprintf("%s Running %s %s · esc to interrupt", spin(s.Now), plural(s.Live.Tools, "command"), clock(s.Now.Sub(s.Live.Started)))
	case s.Live != nil:
		text = fmt.Sprintf("%s Working %s · esc to interrupt", spin(s.Now), clock(s.Now.Sub(s.Live.Started)))
	case s.Busy:
		text = spin(s.Now) + " Starting"
	default:
		return ""
	}

	return tool.Render(ansi.Truncate(text, w, "…"))
}

func footerLine(s state.State, w int) string {
	if !s.Details {
		var parts []string
		fast := ""
		if s.Settings.ServiceTier != "" {
			fast = "fast"
		}
		for _, p := range []string{s.Settings.Model, s.Settings.Effort, fast, home(s.Settings.Workspace)} {
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
		// The hint wins over the left side, which is cut when the line is full.
		room := w - ansi.StringWidth(hint)
		if room > 0 {
			left = ansi.Truncate(left, room-1, "…")
			left += strings.Repeat(" ", room-ansi.StringWidth(left)) + hint
		}

		return dim.Render(ansi.Truncate(left, w, ""))
	}
	if s.Status != "" {
		return warn.Render(ansi.Truncate(" "+s.Status, w, "…"))
	}
	t := s.Totals
	text := fmt.Sprintf(" %s in (%s cached) · %s out · %s · %s (∥%d)", tokens(t.Tokens.InputTokens), tokens(t.Tokens.CachedInputTokens),
		tokens(t.Tokens.OutputTokens), plural(t.Runs, "run"), plural(t.ToolCalls, "tool"), t.MaxParallel)
	if t.ToolBusy > 0 {
		text += fmt.Sprintf(" · overlap %d%%", int(100*t.Overlap/t.ToolBusy))
	}
	hint := "enter send · ctrl+enter now · / commands "
	if gap := w - ansi.StringWidth(text) - ansi.StringWidth(hint); gap > 0 {
		text += strings.Repeat(" ", gap) + hint
	}

	return dim.Render(ansi.Truncate(text, w, ""))
}

func picker(s state.State, f Frame) string {
	scope := "this directory · tab: all"
	if s.Picker.All {
		scope = "all directories · tab: this directory"
	}
	lines := []string{header.Render(ansi.Truncate(fmt.Sprintf(" Resume a session · %s · filter: %s▏", scope, s.Picker.Filter)+strings.Repeat(" ", f.Width), f.Width, ""))}
	list := s.Picker.Filtered()
	if len(list) == 0 {
		empty := "  no sessions match"
		if !s.Picker.All && s.Picker.Filter == "" {
			empty = "  no sessions in this directory · tab shows all · esc starts a new one"
		}
		lines = append(lines, "", dim.Render(empty))
	}
	room := f.Height - 3
	start := max(0, min(s.Picker.Selected-room/2, len(list)-room))
	for i := start; i < len(list) && i < start+room; i++ {
		in := list[i]
		row := fmt.Sprintf(" %s  %-9s %7s  %-11s %-12s ", short(in.ID), age(s.Now, in.LastActivity), plural(in.Runs, "run"), in.Status, in.Model)
		if s.Picker.All {
			row += fmt.Sprintf("%-24s ", ansi.Truncate(home(in.Workspace), 24, "…"))
		}
		row += oneLine(in.FirstPrompt)
		row = ansi.Truncate(row, f.Width, "…")
		if i == s.Picker.Selected {
			row = selected.Render(row + strings.Repeat(" ", max(f.Width-ansi.StringWidth(row), 0)))
		}
		lines = append(lines, row)
	}
	for len(lines) < f.Height-1 {
		lines = append(lines, "")
	}
	lines = append(lines, dim.Render(" type to filter · ↑↓ choose · enter resume · tab this directory/all · esc back"))

	return strings.Join(lines, "\n")
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}

	return fmt.Sprintf("%d %ss", n, noun)
}

func short(id string) string { return id[:min(8, len(id))] }

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

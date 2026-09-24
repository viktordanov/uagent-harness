package render

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// Cache keeps the rendered lines of finished items per width.
type Cache struct {
	entries map[string]cacheEntry
	// maxScroll is how far the last frame could scroll up, or -1 when the
	// frame did not reach the first item (the limit is not known yet).
	maxScroll int
}

// MaxScroll is how far the last frame's transcript could scroll up, or -1
// when unknown.
func (c *Cache) MaxScroll() int { return c.maxScroll }

type cacheEntry struct {
	version, width     int
	reasoning, details bool
	lines              []string
}

func NewCache() *Cache { return &Cache{entries: map[string]cacheEntry{}, maxScroll: -1} }

// view is how items are drawn: the compact default or the detailed view.
type view struct {
	reasoning, details bool
}

// lines returns an item's lines, from the cache when the item is not live.
func (c *Cache) lines(it state.Item, width int, now time.Time, v view) []string {
	if it.Live() {
		return itemLines(it, width, now, v)
	}
	if e, ok := c.entries[it.Key]; ok && e.version == it.Version && e.width == width && e.reasoning == v.reasoning && e.details == v.details {
		return e.lines
	}
	lines := itemLines(it, width, now, v)
	c.entries[it.Key] = cacheEntry{version: it.Version, width: width, reasoning: v.reasoning, details: v.details, lines: lines}

	return lines
}

func itemLines(it state.Item, w int, now time.Time, v view) []string {
	if !v.details {
		if lines, ok := compactLines(it, w, now); ok {
			return lines
		}
	}
	reasoning := v.reasoning
	switch it.Kind {
	case state.KindUser:
		suffix := ""
		switch it.Input {
		case state.InputSent:
			suffix = dim.Render("  sending…")
		case state.InputFailed:
			suffix = bad.Render("  not delivered")
		case state.InputQueued, state.InputDelivered:
		}
		lines := wrapPrefixed(it.Text, w, user.Render("› "), "  ")
		lines[len(lines)-1] += suffix

		return append([]string{""}, lines...)
	case state.KindRun:
		return []string{dim.Render(runRule(it, w, now))}
	case state.KindTurn:
		if it.Pending {
			return []string{dim.Render(fmt.Sprintf("  turn %d  ", it.Turn)) + tool.Render(spin(now)) + dim.Render(" thinking "+clock(now.Sub(it.Started)))}
		}

		return []string{dim.Render(fmt.Sprintf("  turn %d  %s in · %s out · %s", it.Turn, tokens(it.In), tokens(it.Out), secs(it.Duration)))}
	case state.KindTool:
		return []string{toolLine(it, w, now)}
	case state.KindAgent:
		return []string{agentLine(it, w, now)}
	case state.KindAssistant:
		if it.Final {
			return append([]string{"", answer.Render("● answer")}, markdownLines(it.Text, w, "  ", "  ")...)
		}

		return markdownLines(it.Text, w, dim.Render("  · "), "    ")
	case state.KindReasoning:
		if !reasoning {
			return nil
		}

		return styleLines(wrapPrefixed(it.Text, w, "  ~ ", "    "), italic)
	case state.KindNotice:
		style, mark := dim, "  i "
		switch it.Level {
		case session.LevelWarning:
			style, mark = warn, "  ! "
		case session.LevelError:
			style, mark = bad, "  ✗ "
		}
		var out []string
		i := 0
		for part := range strings.SplitSeq(it.Text, "\n") {
			prefix := "    "
			if i == 0 {
				prefix = mark
			}
			out = append(out, styleLines(wrapPrefixed(part, w, prefix, "    "), style)...)
			i++
		}

		return out
	}

	return nil
}

// compactLines draws an item in the compact, Codex-like view. ok is false for
// kinds drawn the same in both views.
func compactLines(it state.Item, w int, now time.Time) ([]string, bool) {
	switch it.Kind {
	case state.KindRun:
		switch it.Status {
		case core.StatusOK, core.StatusRunning:
			return nil, true
		case core.StatusInterrupted:
			return []string{warn.Render("  ■ interrupted")}, true
		case core.StatusTimeout, core.StatusDiskLimit, core.StatusFailed:
		}

		return []string{bad.Render(fmt.Sprintf("  ✗ run ended: %s", it.Status))}, true
	case state.KindTurn:
		return nil, true
	case state.KindTool:
		return []string{compactTool(it, w, now)}, true
	case state.KindAssistant:
		bullet := "• "
		if it.Final {
			bullet = answer.Render("● ")
			return append([]string{""}, markdownLines(it.Text, w, bullet, "  ")...), true
		}

		return markdownLines(it.Text, w, bullet, "  "), true
	case state.KindNotice:
		if it.Level == state.LevelDebug {
			return nil, true
		}
	case state.KindUser, state.KindReasoning:
	}

	return nil, false
}

// compactTool draws a tool call as one Codex-like line: "• Ran <command>".
func compactTool(it state.Item, w int, now time.Time) string {
	verb := it.Name
	if it.Name == "Bash" {
		verb = "Ran"
	}
	var mark, suffix string
	switch it.Tool {
	case state.ToolCalled, state.ToolRunning:
		mark, suffix = tool.Render(spin(now)), dim.Render("  "+clock(now.Sub(it.Started)))
		if it.Name == "Bash" {
			verb = "Running"
		}
	case state.ToolOK:
		mark = dim.Render("•")
		if it.Duration >= time.Second {
			suffix = dim.Render("  " + secs(it.Duration))
		}
	case state.ToolFailed:
		mark, suffix = bad.Render("•"), bad.Render("  "+it.Detail)
	case state.ToolStopped:
		mark, suffix = warn.Render("■"), warn.Render("  stopped")
	}
	head := fmt.Sprintf("%s %s ", mark, bold.Render(verb))
	room := w - ansi.StringWidth(head) - ansi.StringWidth(suffix)

	return head + ansi.Truncate(oneLine(it.Label), max(room, 8), "…") + suffix
}

func runRule(it state.Item, w int, now time.Time) string {
	var text string
	if it.Status == core.StatusRunning {
		text = fmt.Sprintf("── run %s · running %s ", it.RunID, clock(now.Sub(it.Started)))
	} else {
		text = fmt.Sprintf("── run %s · %s · %s · %s tokens ", it.RunID, it.Status, secs(it.Wall), tokens(it.Tokens))
	}
	if pad := w - ansi.StringWidth(text); pad > 0 {
		text += strings.Repeat("─", pad)
	}

	return ansi.Truncate(text, w, "")
}

func toolLine(it state.Item, w int, now time.Time) string {
	var mark, detail string
	switch it.Tool {
	case state.ToolCalled:
		mark, detail = tool.Render(spin(now)), dim.Render("starting")
	case state.ToolRunning:
		mark, detail = tool.Render(spin(now)), dim.Render("running "+clock(now.Sub(it.Started)))
	case state.ToolOK:
		mark, detail = ok.Render("✓"), dim.Render(it.Detail+" · "+secs(it.Duration))
	case state.ToolFailed:
		mark, detail = bad.Render("✗"), bad.Render(it.Detail)+dim.Render(" · "+secs(it.Duration))
	case state.ToolStopped:
		mark, detail = warn.Render("■"), warn.Render("stopped")
	}
	head := fmt.Sprintf("  %s %s  ", mark, tool.Render(it.Name))
	room := w - ansi.StringWidth(head) - ansi.StringWidth(detail) - 2

	return head + ansi.Truncate(it.Label, max(room, 8), "…") + "  " + detail
}

// agentLine draws a subagent: "• agent Ada: running 0:42".
func agentLine(it state.Item, w int, now time.Time) string {
	name := it.Name
	if it.Label != "" {
		name += " (" + it.Label + ")"
	}
	var detail string
	switch it.Detail {
	case engine.AgentRunning:
		detail = tool.Render(spin(now)) + dim.Render(" running "+clock(now.Sub(it.Started)))
	case engine.AgentCompleted:
		detail = ok.Render("done")
	case engine.AgentErrored:
		detail = bad.Render("failed")
	default:
		detail = dim.Render(it.Detail)
	}

	return ansi.Truncate("• agent "+tool.Render(name)+": "+detail, w, "…")
}

// wrapPrefixed wraps text to width w with first and continuation prefixes.
func wrapPrefixed(text string, w int, first, rest string) []string {
	width := max(w-ansi.StringWidth(first), 10)
	var out []string
	for para := range strings.SplitSeq(strings.TrimRight(text, "\n"), "\n") {
		for line := range strings.SplitSeq(ansi.Wrap(para, width, ""), "\n") {
			prefix := rest
			if len(out) == 0 {
				prefix = first
			}
			out = append(out, prefix+line)
		}
	}
	if len(out) == 0 {
		out = []string{first}
	}

	return out
}

func styleLines(lines []string, style interface{ Render(...string) string }) []string {
	for i, l := range lines {
		lines[i] = style.Render(l)
	}

	return lines
}

func spin(now time.Time) string { return spinner[(now.UnixMilli()/100)%int64(len(spinner))] }

func clock(d time.Duration) string {
	d = max(d, 0).Round(time.Second)
	if d >= time.Hour {
		return fmt.Sprintf("%d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
	}

	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

func secs(d time.Duration) string {
	if d >= 2*time.Minute {
		return clock(d)
	}

	return fmt.Sprintf("%.1fs", d.Seconds())
}

// tokens formats a count as 834, 12.4k, or 1.2M.
func tokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}

	return strconv.FormatInt(n, 10)
}

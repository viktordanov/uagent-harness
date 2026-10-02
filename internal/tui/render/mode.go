package render

import (
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"github.com/viktordanov/uah/internal/approval"
	"github.com/viktordanov/uah/internal/sandbox"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/internal/tui/state"
)

// yoloText is how the footer and the header name yolo mode.
var yoloText = approval.ModeYolo.Label() + " mode"

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

// markYolo draws line in base with its "yolo mode" in mark, so the one
// mode that runs everything unasked stands out.
// effortLabel is the effort as the footer and the detailed header show
// it, and whether it is marked in the accent. Off, it is the session's
// effort. With adaptive effort on, idle shows ↓ (1 step) or ⇊ (2 steps);
// while a request is out, the effort it went at: high→low for a lowered
// follow-up, high otherwise. short drops the →low part for narrow widths.
func effortLabel(s state.State) (full, short string, accent bool) {
	e := s.Settings.Effort
	steps := session.AdaptiveSteps(s.Settings.AdaptiveEffort)
	if steps == 0 {
		return e, e, false
	}
	if s.Live != nil && s.Live.Progress.Effort != "" {
		if sent := s.Live.Progress.Effort; sent != e {
			return e + "→" + sent, e, true
		}

		return e, e, true
	}
	e += map[int]string{1: "↓", 2: "⇊"}[steps]

	return e, e, true
}

// span is a byte range of a line in a style of its own.
type span struct {
	from, to int
	style    lipgloss.Style
}

// paint renders line in base, and each span, in order and apart, in its
// own style; a span past the line's end is cut to it.
func paint(line string, base lipgloss.Style, spans ...span) string {
	var b strings.Builder
	at := 0
	for _, sp := range spans {
		from, to := min(max(sp.from, at), len(line)), min(sp.to, len(line))
		for from < to && !utf8.RuneStart(line[from]) { // never inside a character
			from++
		}
		for to < len(line) && !utf8.RuneStart(line[to]) {
			to++
		}
		if from >= to {
			continue
		}
		b.WriteString(base.Render(line[at:from]))
		b.WriteString(sp.style.Render(line[from:to]))
		at = to
	}
	b.WriteString(base.Render(line[at:]))

	return b.String()
}

func markYolo(line string, base, mark lipgloss.Style) string {
	before, after, found := strings.Cut(line, yoloText)
	if !found {
		return base.Render(line)
	}

	return base.Render(before) + mark.Render(yoloText) + base.Render(after)
}

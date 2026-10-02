package render_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/internal/tui/render"
	"github.com/viktordanov/uah/internal/tui/state"
)

// adaptiveAt is base() with adaptive effort value.
func adaptiveAt(value string) state.State {
	next := base().Settings
	next.AdaptiveEffort = value

	return apply(base(), session.SettingsChanged{At: t0, Settings: next})
}

// sending is s with a request out at effort.
func sending(s state.State, effort string) state.State {
	return apply(s,
		core.RunStarted{At: t0, RunID: "r1"},
		core.TurnStarted{At: t0, Turn: 2},
		engine.ModelProgress{At: t0.Add(time.Second), Phase: engine.PhaseWaiting, Effort: effort},
	)
}

// lastLine is the screen's last line at width w, with its styles.
func lastLine(s state.State, w int) string {
	out, _ := render.Screen(s, render.NewCache(render.Amber), render.Frame{Width: w, Height: 24, Composer: "λ ", ComposerHeight: 1})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	return lines[len(lines)-1]
}

// TestFooterShowsAdaptiveEffort: idle, the footer marks the effort ↓ (1
// step) or ⇊ (2 steps); while a request is out, the effort it went at,
// high→low on a lowered follow-up. On, the effort is in the accent. A
// narrow footer drops the →low part first; the detailed header shows the
// same.
func TestFooterShowsAdaptiveEffort(t *testing.T) {
	cases := []struct {
		name  string
		s     state.State
		width int
	}{
		{"idle, off", adaptiveAt("off"), 100},
		{"idle, 1 step", adaptiveAt("1-step"), 100},
		{"idle, 2 steps", adaptiveAt("2-steps"), 100},
		{"working, off", sending(adaptiveAt("off"), "high"), 120},
		{"working, 1 step, a follow-up", sending(adaptiveAt("1-step"), "medium"), 120},
		{"working, 2 steps, a follow-up", sending(adaptiveAt("2-steps"), "low"), 120},
		{"working, 2 steps, a user message", sending(adaptiveAt("2-steps"), "high"), 120},
		{"working, 2 steps, a follow-up, narrow", sending(adaptiveAt("2-steps"), "low"), 96},
		{"idle, 2 steps, narrow", adaptiveAt("2-steps"), 40},
	}
	var b strings.Builder
	for _, c := range cases {
		fmt.Fprintf(&b, "%s:\n%s\n", c.name, strings.TrimRight(ansi.Strip(lastLine(c.s, c.width)), " "))
		c.s.Details = true
		out, _ := render.Screen(c.s, render.NewCache(render.Amber), render.Frame{Width: c.width, Height: 24, Composer: "λ ", ComposerHeight: 1})
		fmt.Fprintf(&b, "%s\n", strings.TrimRight(ansi.Strip(strings.SplitN(out, "\n", 2)[0]), " "))
	}
	golden(t, "adaptive", b.String())

	accent := func(text string) string {
		return lipgloss.NewStyle().Foreground(render.Amber.Accent).Bold(true).Render(text)
	}
	assert.Contains(t, lastLine(adaptiveAt("2-steps"), 100), accent("high⇊"))
	assert.Contains(t, lastLine(sending(adaptiveAt("2-steps"), "low"), 120), accent("high→low"))
	assert.Contains(t, lastLine(sending(adaptiveAt("1-step"), "high"), 100), accent("high"))
	assert.NotContains(t, lastLine(adaptiveAt("off"), 100), accent("high"), "off is dim as before")
	assert.NotContains(t, lastLine(adaptiveAt("2-steps"), 40), "\uFFFD", "a cut effort is not split")
}

// TestFooterKeepsLoweredEffort: while a lowered follow-up is out, a footer
// too full for the whole line cuts its end, the directory, before the
// effort's →medium part, which is the only place the lowering shows.
func TestFooterKeepsLoweredEffort(t *testing.T) {
	s := sending(adaptiveAt("1-step"), "medium")
	s.Settings.Workspace = "/home/colleague/src/github.com/team/service"
	for _, w := range []int{80, 100, 120} {
		line := ansi.Strip(lastLine(s, w))
		assert.Contains(t, line, "high→medium", "width %d: %q", w, line)
		s.Details = true
		out, _ := render.Screen(s, render.NewCache(render.Amber), render.Frame{Width: w, Height: 24, Composer: "λ ", ComposerHeight: 1})
		assert.Contains(t, ansi.Strip(strings.SplitN(out, "\n", 2)[0]), "high→medium", "the header at width %d", w)
		s.Details = false
	}
}

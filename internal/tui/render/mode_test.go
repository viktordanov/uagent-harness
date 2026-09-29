package render_test

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/render"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// footer is the screen's last line.
func footer(s state.State) string {
	lines := strings.Split(strings.TrimRight(screen(s, ""), "\n"), "\n")

	return lines[len(lines)-1]
}

func TestFooterShowsThePermissionMode(t *testing.T) {
	for mode, want := range map[approval.Mode]string{
		approval.ModeReadOnly:  "gpt-6-sol high · read only mode · /workspace/proj",
		approval.ModeWorkspace: "gpt-6-sol high · workspace mode · /workspace/proj",
		approval.ModeAuto:      "gpt-6-sol high · auto mode · /workspace/proj",
		approval.ModeYolo:      "gpt-6-sol high · yolo mode · /workspace/proj",
	} {
		t.Run(string(mode), func(t *testing.T) {
			s := apply(base(), session.SettingsChanged{Settings: base().Settings.WithMode(mode)})

			assert.Contains(t, footer(s), want)

			s.Details = true
			assert.Contains(t, strings.SplitN(screen(s, ""), "\n", 2)[0], "high · "+mode.Label()+" mode ·", "the detailed header")
		})
	}
}

func TestShiftTabThenFooter(t *testing.T) {
	s, effects := state.Reduce(base(), state.CycleMode{})
	next := effects[0].(state.EffSetSettings).Settings
	s = apply(s, session.SettingsChanged{Settings: next, Applied: session.AppliedLive})

	assert.Contains(t, footer(s), "· auto mode ·")
}

// TestYoloModeInTheWarningColor: the footer draws "yolo mode" in the
// theme's warning color, apart from the dim rest of the line.
func TestYoloModeInTheWarningColor(t *testing.T) {
	s := apply(base(), session.SettingsChanged{Settings: base().Settings.WithMode(approval.ModeYolo)})
	out, _ := render.Screen(s, render.NewCache(render.Amber), render.Frame{Width: 100, Height: 24, Composer: "λ ", ComposerHeight: 1})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	last := lines[len(lines)-1]

	warn := lipgloss.NewStyle().Foreground(render.Amber.Warn).Render("yolo mode")
	assert.Contains(t, last, warn)

	s = apply(base(), session.SettingsChanged{Settings: base().Settings.WithMode(approval.ModeAuto)})
	out, _ = render.Screen(s, render.NewCache(render.Amber), render.Frame{Width: 100, Height: 24, Composer: "λ ", ComposerHeight: 1})
	assert.NotContains(t, out, lipgloss.NewStyle().Foreground(render.Amber.Warn).Render("auto mode"))
}

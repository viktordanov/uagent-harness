package render_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// footer is the screen's last line.
func footer(s state.State) string {
	lines := strings.Split(strings.TrimRight(screen(s, ""), "\n"), "\n")

	return lines[len(lines)-1]
}

func TestFooterShowsThePermissionMode(t *testing.T) {
	for mode, want := range map[approval.Mode]string{
		approval.ModeReadOnly:   "gpt-6-sol high · read only mode · /workspace/proj",
		approval.ModeWorkspace:  "gpt-6-sol high · workspace mode · /workspace/proj",
		approval.ModeAuto:       "gpt-6-sol high · auto mode · /workspace/proj",
		approval.ModeFullAccess: "gpt-6-sol high · full access mode · /workspace/proj",
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

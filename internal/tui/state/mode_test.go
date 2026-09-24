package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

func TestReduce_ShiftTabCyclesThePermissionMode(t *testing.T) {
	tests := []struct {
		from, want approval.Mode
		sandbox    string
	}{
		{from: approval.ModeWorkspace, want: approval.ModeAuto, sandbox: "workspace-write"},
		{from: approval.ModeAuto, want: approval.ModeReadOnly, sandbox: "read-only"},
		{from: approval.ModeReadOnly, want: approval.ModeWorkspace, sandbox: "workspace-write"},
		{from: approval.ModeFullAccess, want: approval.ModeReadOnly, sandbox: "read-only"},
		{from: "", want: approval.ModeAuto, sandbox: "workspace-write"},
	}
	for _, tt := range tests {
		t.Run(string(tt.from), func(t *testing.T) {
			s, _ := apply(opened(), session.SettingsChanged{Settings: settings().WithMode(tt.from)})
			if tt.from == "" {
				s = opened()
			}

			_, effects := apply(s, state.CycleMode{})

			require.Len(t, effects, 1)
			next := effects[0].(state.EffSetSettings).Settings
			assert.Equal(t, tt.want, next.Mode)
			assert.Equal(t, tt.sandbox, next.Sandbox)
			assert.Equal(t, s.Settings.Model, next.Model, "only the mode changes")
		})
	}
}

func TestReduce_ModeChangeNotice(t *testing.T) {
	s, _ := apply(opened(), session.SettingsChanged{Settings: settings().WithMode(approval.ModeAuto), Applied: session.AppliedLive})

	assert.Equal(t, approval.ModeAuto, s.Settings.Mode)
	last := s.Items[len(s.Items)-1]
	assert.Contains(t, last.Text, "auto mode: commands write the workspace; the auto-reviewer approves or declines the rest without asking you. Applies now.")
}

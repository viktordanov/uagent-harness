package approval_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/approval"
	"github.com/viktordanov/uah/internal/sandbox"
)

// TestModes pins the mapping: each mode's sandbox, who decides, and the
// shift+tab cycle, which has yolo after auto only with --yolo.
func TestModes(t *testing.T) {
	tests := []struct {
		mode      approval.Mode
		sandbox   sandbox.Mode
		reviewer  bool
		next      approval.Mode
		nextYolo  approval.Mode
		asksNoOne bool
	}{
		{approval.ModeReadOnly, sandbox.ReadOnly, false, approval.ModeWorkspace, approval.ModeWorkspace, false},
		{approval.ModeWorkspace, sandbox.WorkspaceWrite, false, approval.ModeAuto, approval.ModeAuto, false},
		{approval.ModeAuto, sandbox.WorkspaceWrite, true, approval.ModeReadOnly, approval.ModeYolo, false},
		{approval.ModeYolo, sandbox.FullAccess, false, approval.ModeReadOnly, approval.ModeReadOnly, true},
		{"", sandbox.WorkspaceWrite, false, approval.ModeAuto, approval.ModeAuto, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			assert.Equal(t, tt.sandbox, tt.mode.Sandbox())
			assert.Equal(t, tt.reviewer, tt.mode.ReviewerDecides())
			assert.Equal(t, tt.asksNoOne, tt.mode.AsksNoOne())
			assert.Equal(t, tt.next, tt.mode.Next(false), "without --yolo")
			assert.Equal(t, tt.nextYolo, tt.mode.Next(true), "with --yolo")
		})
	}
	for _, m := range []sandbox.Mode{sandbox.ReadOnly, sandbox.WorkspaceWrite, sandbox.FullAccess} {
		assert.Equal(t, m, approval.ModeFor(m).Sandbox(), "ModeFor inverts Sandbox")
	}
}

func TestParseMode(t *testing.T) {
	for _, name := range []string{"read-only", "workspace", "auto", "yolo"} {
		m, err := approval.ParseMode(name)
		require.NoError(t, err)
		assert.Equal(t, approval.Mode(name), m)
	}
	m, err := approval.ParseMode("")
	require.NoError(t, err)
	assert.Equal(t, approval.ModeWorkspace, m)
	_, err = approval.ParseMode("full-access")
	require.ErrorContains(t, err, `invalid permission mode "full-access"`)
}

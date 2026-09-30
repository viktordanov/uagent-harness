package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/approval"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/internal/tui/state"
)

func TestWindowTitle(t *testing.T) {
	s := state.New(t0)
	s.Title = true
	assert.Equal(t, "uah", s.WindowTitle(), "no session yet")

	s, _ = apply(s, session.SessionOpened{At: t0, ID: "s1", Settings: settings()})
	assert.Equal(t, "uah · workspace", s.WindowTitle())

	s, _ = apply(s, session.InputQueued{At: t0, Input: core.UserInput{ID: "i1", Text: "hi"}})
	assert.Equal(t, "uah · working · workspace", s.WindowTitle())

	s, _ = apply(s, session.ApprovalRequested{At: t0, ID: "a1", Command: "git push"})
	assert.Equal(t, "uah · approve? · workspace", s.WindowTitle())

	s, _ = apply(s, session.ApprovalResolved{At: t0, ID: "a1", Decision: approval.Approve}, session.Idle{At: t0})
	assert.Equal(t, "uah · workspace", s.WindowTitle())

	s, _ = apply(s, session.SettingsChanged{At: t0, Settings: settings().WithMode(approval.ModeYolo)})
	assert.Equal(t, "uah · yolo · workspace", s.WindowTitle(), "yolo mode shows in the title")
	s, _ = apply(s, session.SettingsChanged{At: t0, Settings: settings()})

	s.Title = false
	s, _ = apply(s, session.InputQueued{At: t0, Input: core.UserInput{ID: "i2", Text: "again"}})
	assert.Empty(t, s.WindowTitle(), "off: uah leaves the title alone")
}

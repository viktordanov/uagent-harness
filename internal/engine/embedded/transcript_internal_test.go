package embedded

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"
)

// TestTranscript_PerSession keeps each session's auto-review transcript
// apart, so a subagent's review sees its own messages and resetting one
// reviewer's breaker leaves the other's alone.
func TestTranscript_PerSession(t *testing.T) {
	e := New(Config{})
	parentResets := 0
	e.transcript("parent").onUser = func() { parentResets++ }
	e.transcript("parent").observe(core.UserMessage{Text: "the user's request"})
	e.transcript("child").observe(core.UserMessage{Text: "the parent's task for the child"})

	users, _ := e.transcript("parent").snapshot()
	assert.Equal(t, []string{"the user's request"}, users)
	users, _ = e.transcript("child").snapshot()
	assert.Equal(t, []string{"the parent's task for the child"}, users)
	assert.Equal(t, 1, parentResets, "the child's message did not reset the parent's reviewer")
	assert.Same(t, e.transcript("parent"), e.transcript("parent"))
}

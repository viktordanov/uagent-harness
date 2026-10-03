package state_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/tui/state"
)

// TestPrepared_IsANote shows the prepared context as a line, not as the
// user's message: a developer message, and the user message an earlier
// version sent.
func TestPrepared_IsANote(t *testing.T) {
	text := "<context_preparation>\n" + strings.Repeat("x", 2000) + "\n</context_preparation>"
	for _, ev := range []core.Event{
		core.DeveloperMessage{At: t0, ID: "p1", Text: text},
		core.UserMessage{At: t0, ID: "p1", Text: text},
	} {
		s, _ := apply(opened(), ev)
		require.Equal(t, []state.Kind{state.KindNotice}, kinds(s))
		assert.Equal(t, "uah prepared the session's context (2.0 KB)", s.Items[0].Text)
	}
	s, _ := apply(opened(), core.DeveloperMessage{At: t0, ID: "d1", Text: "other context"})
	assert.Equal(t, "uah sent the model context (0.0 KB)", s.Items[0].Text)
}

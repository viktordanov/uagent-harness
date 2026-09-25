package render_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// TestStreamedAnswer: an answer the model is still writing is drawn as the
// final answer is, markdown and all, and the status line says the model
// is writing.
func TestStreamedAnswer(t *testing.T) {
	// A state shares its transcript with the states reduced from it, so
	// each case starts from its own.
	live := func() state.State { return apply(base(), core.RunStarted{At: t0, RunID: "r1"}, core.TurnStarted{At: t0, Turn: 1}) }
	text := "The **fix** is in:\n\n```go\nreturn nil"
	streaming := apply(live(), engine.TextDelta{At: t0, ItemID: "msg", Text: text[:10], Final: true}, engine.TextDelta{At: t0, ItemID: "msg", Text: text[10:]}, state.Tick{Now: t0})
	final := apply(live(), core.AssistantMessage{At: t0, Turn: 1, Text: text, Final: true}, state.Tick{Now: t0})

	got := screen(streaming, "")
	assert.Contains(t, got, "The fix is in:", "markdown, drawn while it streams")
	assert.Contains(t, got, "return nil", "an open code block shows its lines so far")
	assert.Contains(t, got, "Writing")
	assert.NotContains(t, got, "Thinking")
	assert.Equal(t, screen(apply(final, state.ToggleDetails{}), ""), screen(apply(streaming, state.ToggleDetails{}), ""), "the detailed view draws them alike")

	thinking := apply(live(), engine.ReasoningDelta{At: t0, ItemID: "rs", Text: "Considering"}, state.Tick{Now: t0})
	assert.Contains(t, screen(thinking, ""), "Thinking", "reasoning is not writing the answer")
}

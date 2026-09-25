package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// kindsAndTexts are the transcript's assistant and reasoning items as
// "kind:text", with a * for one still streaming.
func kindsAndTexts(s state.State) []string {
	var out []string
	for _, it := range s.Items {
		var kind string
		switch it.Kind {
		case state.KindAssistant:
			kind = "answer"
		case state.KindReasoning:
			kind = "reasoning"
		default:
			continue
		}
		if it.Streaming {
			kind += "*"
		}
		out = append(out, kind+":"+it.Text)
	}

	return out
}

func running() state.State {
	s, _ := apply(opened(), core.RunStarted{At: t0, RunID: "r1"}, core.TurnStarted{At: t0, Turn: 1})

	return s
}

// TestReduce_StreamedAnswer: deltas grow a live item in place, and the
// final message replaces its text in the same place, so a difference
// between the two corrects itself.
func TestReduce_StreamedAnswer(t *testing.T) {
	s, _ := apply(running(),
		engine.ReasoningDelta{At: t0, ItemID: "rs", Text: "Think"},
		engine.ReasoningDelta{At: t0, ItemID: "rs", Text: "ing"},
		engine.TextDelta{At: t0, ItemID: "msg", Text: "Hel", Final: true},
		engine.TextDelta{At: t0, ItemID: "msg", Text: "lo"},
	)
	assert.Equal(t, []string{"reasoning*:Thinking", "answer*:Hello"}, kindsAndTexts(s))
	n := len(s.Items)
	streamed := s.Items[n-1]
	assert.True(t, streamed.Final, "the message is the final answer")
	assert.Positive(t, streamed.Version, "each delta changes the item, so the renderer redraws it")

	s, _ = apply(s,
		core.ModelResponded{At: t0, Turn: 1},
		core.ReasoningSummary{At: t0, Turn: 1, Text: "Thinking."},
		core.AssistantMessage{At: t0, Turn: 1, Text: "Hello!", Final: true},
	)
	assert.Equal(t, []string{"reasoning:Thinking.", "answer:Hello!"}, kindsAndTexts(s), "the recorded text wins")
	assert.Len(t, s.Items, n, "the final messages took the streamed items' places")
	final, ok := s.Item(streamed.Key)
	require.True(t, ok)
	assert.Greater(t, final.Version, streamed.Version)

	s, _ = apply(s, core.AssistantMessage{At: t0, Turn: 2, Text: "later"})
	assert.Equal(t, "answer:later", kindsAndTexts(s)[2], "without a stream, a message is a new item")
}

// TestReduce_StreamReset: a reset drops the streamed items, and the next
// attempt's text starts afresh; items the stream did not make stay.
func TestReduce_StreamReset(t *testing.T) {
	s, _ := apply(running(), engine.TextDelta{At: t0, ItemID: "msg-a", Text: "lost words"})
	before := len(s.Items)
	s, _ = apply(s, engine.StreamReset{At: t0})
	assert.Empty(t, kindsAndTexts(s))
	assert.Len(t, s.Items, before-1)
	_, ok := s.Item("turn:r1:1")
	assert.True(t, ok, "the index follows the removal")

	s, _ = apply(s, engine.TextDelta{At: t0, ItemID: "msg-b", Text: "kept"}, core.AssistantMessage{At: t0, Turn: 1, Text: "kept words"})
	assert.Equal(t, []string{"answer:kept words"}, kindsAndTexts(s))
}

// TestReduce_StreamLeftoversEndWithTheRun: streamed text no final message
// claimed, as when a run stops mid-answer, goes when the run finishes.
func TestReduce_StreamLeftoversEndWithTheRun(t *testing.T) {
	s, _ := apply(running(), engine.ReasoningDelta{At: t0, ItemID: "rs", Text: "half a thought"})
	s, _ = apply(s, core.RunFinished{At: t0, Result: core.Result{Request: core.Request{RunID: "r1"}, Status: core.StatusInterrupted}})
	assert.Empty(t, kindsAndTexts(s))
}

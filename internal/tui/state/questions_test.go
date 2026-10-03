package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/internal/tui/state"
)

var twoQuestions = []engine.Question{
	{ID: "strategy", Header: "Migration", Question: "Which way?", Options: []engine.QuestionOption{
		{Label: "Expand and contract (Recommended)", Description: "Safe."}, {Label: "Rename in place", Description: "Quick."},
	}},
	{ID: "rollout", Header: "Rollout", Question: "When?", Options: []engine.QuestionOption{
		{Label: "Next deploy", Description: "With the code."}, {Label: "By hand", Description: "Later."},
	}},
}

func asked(t *testing.T) state.State {
	t.Helper()
	s, _ := apply(state.New(t0),
		session.SessionOpened{At: t0, ID: "s1", Settings: settings()},
		core.RunStarted{At: t0, RunID: "r1"},
		session.QuestionsAsked{At: t0, ID: "q1", CallID: "c1", Questions: twoQuestions},
	)
	s.Title = true

	return s
}

func pending(t *testing.T, s state.State) *state.Questions {
	t.Helper()
	q, ok := s.PendingQuestions()
	require.True(t, ok)

	return q
}

// TestQuestions_Answer: ↑↓ choose, enter answers and shows the next
// question, the own-answer row takes the composer, and the last answer
// sends them all in Codex's encoding, once.
func TestQuestions_Answer(t *testing.T) {
	s := asked(t)
	assert.Equal(t, 0, pending(t, s).Current)
	assert.False(t, pending(t, s).Typing(), "the options take no text")
	wait, _ := s.CurrentWait()
	assert.Equal(t, "Waiting for your answer", wait.What)
	assert.Equal(t, "uah · answer? · workspace", s.WindowTitle())

	s, effects := apply(s, state.QuestionMove{Delta: 1}, state.QuestionAnswer{})
	assert.Equal(t, []state.Effect{state.EffSetDraft{}, state.EffSetDraft{}}, effects)
	assert.Equal(t, 1, pending(t, s).Current)

	s, effects = apply(s, state.QuestionMove{Delta: -1}) // wraps to "Type your own answer"
	assert.Equal(t, []state.Effect{state.EffSetDraft{}}, effects)
	assert.True(t, pending(t, s).OwnRow())
	assert.True(t, pending(t, s).Typing(), "the composer holds the answer")

	_, effects = apply(s, state.QuestionAnswer{Draft: "  "})
	assert.Empty(t, effects, "nothing typed yet")
	s, effects = apply(s, state.QuestionAnswer{Draft: " after Friday "})
	want := engine.Answers{
		"strategy": {Answers: []string{"Rename in place"}},
		"rollout":  {Answers: []string{"None of the above", "user_note: after Friday"}},
	}
	assert.Equal(t, []state.Effect{state.EffSetDraft{}, state.EffAnswerQuestions{ID: "q1", Answers: want}}, effects)

	_, effects = apply(s, state.QuestionAnswer{}, state.QuestionPick{Number: 1}, state.QuestionMove{Delta: 1}, state.QuestionNote{})
	assert.Empty(t, effects, "sent once")

	s, _ = apply(s, session.QuestionsAnswered{At: t0, ID: "q1", Answers: want})
	_, ok := s.PendingQuestions()
	assert.False(t, ok)
}

// TestQuestions_Notes: n writes a note on the chosen option in the
// composer; enter keeps it, esc drops it, and the answer carries the
// chosen option's note only.
func TestQuestions_Notes(t *testing.T) {
	s, effects := apply(asked(t), state.QuestionNote{})
	assert.Equal(t, []state.Effect{state.EffSetDraft{}}, effects)
	q := pending(t, s)
	assert.Equal(t, 0, q.Noting)
	assert.True(t, q.Typing())

	s, effects = apply(s, state.QuestionMove{Delta: 1}, state.QuestionPick{Number: 2})
	assert.Empty(t, effects, "the choice holds while the note is written")
	s, effects = apply(s, state.QuestionAnswer{Draft: " keep a backup "})
	assert.Equal(t, []state.Effect{state.EffSetDraft{}}, effects, "enter keeps the note, and the question waits")
	q = pending(t, s)
	assert.Equal(t, -1, q.Noting)
	assert.Equal(t, "keep a backup", q.Notes[0][0])
	assert.False(t, q.Done[0])

	s, effects = apply(s, state.QuestionNote{})
	assert.Equal(t, []state.Effect{state.EffSetDraft{Text: "keep a backup"}}, effects, "the note again, to edit")
	s, _ = apply(s, state.QuestionDismiss{})
	assert.Equal(t, "keep a backup", pending(t, s).Notes[0][0], "esc drops the edit, not the note")
	assert.Empty(t, s.Live.Stopping, "esc on a note does not interrupt")

	s, _ = apply(s, state.QuestionMove{Delta: 1}, state.QuestionNote{}, state.QuestionAnswer{Draft: "only if quiet"})
	s, effects = apply(s, state.QuestionMove{Delta: -1}, state.QuestionAnswer{}, state.QuestionPick{Number: 2})
	require.Len(t, effects, 5)
	assert.Equal(t, engine.Answers{
		"strategy": {Answers: []string{"Expand and contract (Recommended)", "user_note: keep a backup"}},
		"rollout":  {Answers: []string{"By hand"}},
	}, effects[4].(state.EffAnswerQuestions).Answers, "the chosen option's note, not another's")
	_ = s
}

// TestQuestions_SwitchKeepsOwnAnswers: tab and shift+tab go between the
// questions, each keeping its own answer; a number picks and answers, the
// last number takes the own-answer row.
func TestQuestions_SwitchKeepsOwnAnswers(t *testing.T) {
	s, effects := apply(asked(t), state.QuestionPick{Number: 3})
	assert.Equal(t, []state.Effect{state.EffSetDraft{}}, effects, "the own-answer row, nothing sent")
	s, effects = apply(s, state.QuestionSwitch{Delta: 1, Draft: "first words"})
	assert.Equal(t, []state.Effect{state.EffSetDraft{}}, effects)
	s, effects = apply(s, state.QuestionSwitch{Delta: 1})
	assert.Equal(t, []state.Effect{state.EffSetDraft{Text: "first words"}}, effects, "it wraps around, with the words")
	_, effects = apply(s, state.QuestionPick{Number: 9})
	assert.Empty(t, effects, "no such row")

	s, _ = apply(s, state.QuestionAnswer{Draft: "first words"})
	_, effects = apply(s, state.QuestionPick{Number: 1})
	require.Len(t, effects, 3)
	assert.Equal(t, engine.Answers{
		"strategy": {Answers: []string{"None of the above", "user_note: first words"}},
		"rollout":  {Answers: []string{"Next deploy"}},
	}, effects[2].(state.EffAnswerQuestions).Answers)
}

// TestQuestions_Dismiss: esc interrupts the run, as in Codex; the session
// then cancels the questions and the picker closes.
func TestQuestions_Dismiss(t *testing.T) {
	s, effects := apply(asked(t), state.QuestionDismiss{})
	assert.Equal(t, []state.Effect{state.EffInterrupt{}}, effects)
	wait, _ := s.CurrentWait()
	assert.Equal(t, "Stopping", wait.What)

	s, _ = apply(s, session.QuestionsAnswered{At: t0, ID: "q1", Canceled: true})
	_, ok := s.PendingQuestions()
	assert.False(t, ok)
}

// TestQuestions_ToolLine: the call reads ASK with its headers, or its one
// question, and the answers go under it, an answer in the user's own
// words alone.
func TestQuestions_ToolLine(t *testing.T) {
	s, _ := apply(asked(t),
		core.ToolCalled{At: t0, CallID: "c1", Name: engine.QuestionToolName, Arguments: `{"questions":[{"id":"a","header":"Migration","question":"Which?"},{"id":"b","header":"Rollout","question":"When?"}]}`},
		core.ToolCalled{At: t0, CallID: "c2", Name: engine.QuestionToolName, Arguments: `{"questions":[{"id":"a","header":"Migration","question":"Which   way?"}]}`},
		engine.QuestionsAnswered{At: t0, CallID: "c1", Questions: twoQuestions, Answers: engine.Answers{
			"strategy": {Answers: []string{"Rename in place", "user_note: keep a backup"}}, "rollout": {Answers: []string{"None of the above", "user_note: Friday"}},
		}},
	)
	one, _ := s.Item("call:c1")
	assert.Equal(t, "ASK", one.Verb)
	assert.Equal(t, "Migration, Rollout", one.Parts[0].Text)
	assert.Equal(t, []string{"Migration: Rename in place · keep a backup", "Rollout: Friday"}, one.Answers)
	two, _ := s.Item("call:c2")
	assert.Equal(t, "Which way?", two.Parts[0].Text)
	s, _ = apply(s, engine.QuestionsAnswered{At: t0, CallID: "c2", Questions: twoQuestions[:1], Answers: engine.Answers{"strategy": {Answers: []string{}}}})
	two, _ = s.Item("call:c2")
	assert.Equal(t, []string{"Migration: no answer"}, two.Answers)
}

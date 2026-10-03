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

// TestQuestions_Answer: ↑↓ choose, enter answers and shows the next
// question with its own note, and the last answer sends them all in
// Codex's encoding, once.
func TestQuestions_Answer(t *testing.T) {
	s := asked(t)
	q, ok := s.PendingQuestions()
	require.True(t, ok)
	assert.Equal(t, 0, q.Current)
	wait, _ := s.CurrentWait()
	assert.Equal(t, "Waiting for your answer", wait.What)
	assert.Equal(t, "uah · answer? · workspace", s.WindowTitle())

	s, effects := apply(s, state.QuestionMove{Delta: 1}, state.QuestionAnswer{Draft: "  keep a backup  "})
	assert.Equal(t, []state.Effect{state.EffSetDraft{Text: ""}}, effects, "the second question's empty note")
	q, _ = s.PendingQuestions()
	assert.Equal(t, 1, q.Current)

	s, effects = apply(s, state.QuestionMove{Delta: -1}) // wraps to "None of the above"
	assert.Empty(t, effects)
	q, _ = s.PendingQuestions()
	assert.Equal(t, 2, q.Choice[1])

	s, effects = apply(s, state.QuestionAnswer{Draft: "after Friday"})
	want := engine.Answers{
		"strategy": {Answers: []string{"Rename in place", "user_note: keep a backup"}},
		"rollout":  {Answers: []string{"None of the above", "user_note: after Friday"}},
	}
	assert.Equal(t, []state.Effect{state.EffSetDraft{}, state.EffAnswerQuestions{ID: "q1", Answers: want}}, effects)

	_, effects = apply(s, state.QuestionAnswer{}, state.QuestionPick{Number: 1}, state.QuestionMove{Delta: 1})
	assert.Empty(t, effects, "sent once")

	s, _ = apply(s, session.QuestionsAnswered{At: t0, ID: "q1", Answers: want})
	_, ok = s.PendingQuestions()
	assert.False(t, ok)
}

// TestQuestions_TypingAnswersInOwnWords: typing on a row the user did not
// choose moves to "None of the above"; on a chosen row the text is a note.
func TestQuestions_TypingAnswersInOwnWords(t *testing.T) {
	s, _ := apply(asked(t), state.DraftChanged{Draft: "u"})
	q, _ := s.PendingQuestions()
	assert.Equal(t, 2, q.Choice[0])

	s, _ = apply(asked(t), state.QuestionMove{Delta: 1}, state.DraftChanged{Draft: "u"})
	q, _ = s.PendingQuestions()
	assert.Equal(t, 1, q.Choice[0], "a chosen row keeps the text as its note")
}

// TestQuestions_SwitchKeepsNotes: tab and shift+tab go between the
// questions, each with its own note, and a number picks and answers.
func TestQuestions_SwitchKeepsNotes(t *testing.T) {
	s, effects := apply(asked(t), state.QuestionSwitch{Delta: 1, Draft: "first note"})
	assert.Equal(t, []state.Effect{state.EffSetDraft{Text: ""}}, effects)
	s, effects = apply(s, state.QuestionSwitch{Delta: 1, Draft: "second note"})
	assert.Equal(t, []state.Effect{state.EffSetDraft{Text: "first note"}}, effects, "it wraps around")
	s, effects = apply(s, state.QuestionPick{Number: 2})
	assert.Equal(t, []state.Effect{state.EffSetDraft{Text: "second note"}}, effects)
	_, effects = apply(s, state.QuestionPick{Number: 9})
	assert.Empty(t, effects, "no such row")

	_, effects = apply(s, state.QuestionPick{Number: 1})
	require.Len(t, effects, 2)
	assert.Equal(t, engine.Answers{
		"strategy": {Answers: []string{"Rename in place", "user_note: first note"}},
		"rollout":  {Answers: []string{"Next deploy", "user_note: second note"}},
	}, effects[1].(state.EffAnswerQuestions).Answers)
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
// question, and the answers go under it.
func TestQuestions_ToolLine(t *testing.T) {
	s, _ := apply(asked(t),
		core.ToolCalled{At: t0, CallID: "c1", Name: engine.QuestionToolName, Arguments: `{"questions":[{"id":"a","header":"Migration","question":"Which?"},{"id":"b","header":"Rollout","question":"When?"}]}`},
		core.ToolCalled{At: t0, CallID: "c2", Name: engine.QuestionToolName, Arguments: `{"questions":[{"id":"a","header":"Migration","question":"Which   way?"}]}`},
		engine.QuestionsAnswered{At: t0, CallID: "c1", Questions: twoQuestions, Answers: engine.Answers{
			"strategy": {Answers: []string{"Rename in place"}}, "rollout": {Answers: []string{}},
		}},
	)
	one, _ := s.Item("call:c1")
	assert.Equal(t, "ASK", one.Verb)
	assert.Equal(t, "Migration, Rollout", one.Parts[0].Text)
	assert.Equal(t, []string{"Migration: Rename in place", "Rollout: no answer"}, one.Answers)
	two, _ := s.Item("call:c2")
	assert.Equal(t, "Which way?", two.Parts[0].Text)
}

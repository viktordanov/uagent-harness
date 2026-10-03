package embedded

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/engine"
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

// TestTranscript_KeepsAnswers gives the auto-reviewer the user's answers to
// the agent's questions as their own words, as Codex's guardian gets
// verified answers: the question, the chosen option, and the answer.
func TestTranscript_KeepsAnswers(t *testing.T) {
	tr := newTranscript()
	tr.observe(engine.QuestionsAnswered{
		Questions: []engine.Question{
			{ID: "drop", Question: "Drop the old table?", Options: []engine.QuestionOption{{Label: "Drop it", Description: "Deletes users_old."}, {Label: "Keep it", Description: "Leaves it."}}},
			{ID: "skipped", Question: "Anything else?", Options: []engine.QuestionOption{{Label: "No", Description: "Done."}}},
		},
		Answers: engine.Answers{"drop": {Answers: []string{"Drop it", "user_note: after the backup"}}, "skipped": {Answers: []string{}}},
	})
	tr.observe(engine.QuestionsAnswered{Questions: []engine.Question{{ID: "a", Question: "Which?"}}, Answers: engine.Answers{}})

	users, _ := tr.snapshot()
	assert.Equal(t, []string{"Question: Drop the old table?\nDrop it: Deletes users_old.\nAnswer: Drop it\nuser_note: after the backup"}, users)
}

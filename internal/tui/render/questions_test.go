package render_test

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/internal/tui/render"
	"github.com/viktordanov/uah/internal/tui/state"
)

// migration are a request_user_input call's questions, as a model asks
// them.
var migration = []engine.Question{
	{ID: "strategy", Header: "Migration", Question: "The users table can change three ways. Which should the migration take?", Options: []engine.QuestionOption{
		{Label: "Expand and contract (Recommended)", Description: "Add the new column, backfill it, and drop the old one in a later release."},
		{Label: "Rename in place", Description: "One migration; the table is locked for about a minute."},
		{Label: "Copy and swap", Description: "Build a new table and swap it in; needs twice the disk for a while."},
	}},
	{ID: "rollout", Header: "Rollout", Question: "When should it run?", Options: []engine.QuestionOption{
		{Label: "Next deploy", Description: "Ships with the code."},
		{Label: "By hand", Description: "You run it in a quiet hour."},
	}},
}

const migrationArgs = `{"questions":[{"id":"strategy","header":"Migration","question":"Which?","options":[]},{"id":"rollout","header":"Rollout","question":"When?","options":[]}]}`

// asking is a live run whose agent asked the questions.
func asking() state.State {
	s := apply(base(),
		session.InputQueued{At: t0, Input: core.UserInput{ID: "m", Text: "Plan the users table migration"}},
		session.InputSent{At: t0, IDs: []string{"m"}},
		core.RunStarted{At: t0, RunID: "20260924-120000-3f2a1b2c"},
		core.ToolCalled{At: t0, CallID: "c1", Name: engine.QuestionToolName, Arguments: migrationArgs},
		core.ToolStarted{At: t0, CallID: "c1"},
		session.QuestionsAsked{At: t0, ID: "q1", CallID: "c1", Questions: migration},
	)
	s.Now = t0.Add(12 * time.Second)

	return s
}

func TestScreen_Questions(t *testing.T) {
	s := asking()
	golden(t, "questions", screen(s, ""))

	s = apply(s, state.QuestionMove{Delta: 1})
	golden(t, "questions-moved", screen(s, ""))

	// The first answer shows the second question, its tab marked done.
	s = apply(s, state.QuestionAnswer{Draft: ""})
	golden(t, "questions-second", screen(s, "after the Friday backup"))

	// Typing chose "None of the above".
	s = apply(s, state.DraftChanged{Draft: "after the Friday backup"})
	assert.Contains(t, screen(s, "after the Friday backup"), "› 3. None of the above")
}

// TestScreen_QuestionsNarrow: at 40 columns every line fits, the labels
// and descriptions cut with "…".
func TestScreen_QuestionsNarrow(t *testing.T) {
	out, _ := render.Screen(asking(), render.NewCache(render.Amber), render.Frame{Width: 40, Height: 24, Composer: "λ ", ComposerHeight: 1})
	for line := range strings.SplitSeq(out, "\n") {
		assert.LessOrEqual(t, ansi.StringWidth(line), 40, "%q", ansi.Strip(line))
	}
	golden(t, "questions-narrow", screenAt(asking(), "", 40))
}

// TestScreen_QuestionsAnswered: the call's line, with the answers under
// it, once the answers are recorded.
func TestScreen_QuestionsAnswered(t *testing.T) {
	s := apply(asking(),
		session.QuestionsAnswered{At: t0, ID: "q1"},
		engine.QuestionsAnswered{At: t0, CallID: "c1", Questions: migration, Answers: engine.Answers{
			"strategy": {Answers: []string{"Expand and contract (Recommended)"}},
			"rollout":  {Answers: []string{engine.OtherAnswer, engine.NotePrefix + "after the Friday backup"}},
		}},
		core.ToolFinished{At: t0.Add(9 * time.Second), CallID: "c1", Name: engine.QuestionToolName, OK: true, Detail: "completed", Duration: 9 * time.Second},
	)
	golden(t, "questions-answered", screen(s, ""))
}

func screenAt(s state.State, draft string, w int) string {
	out, _ := render.Screen(s, render.NewCache(render.Amber), render.Frame{Width: w, Height: 24, Composer: "λ " + draft, ComposerHeight: 1, Draft: draft})
	lines := strings.Split(ansi.Strip(out), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}

	return strings.Join(lines, "\n") + "\n"
}

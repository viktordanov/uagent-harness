package embedded_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/engine/embedded"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/testing/fakellm"
)

// migrationQuestions is a request_user_input call's arguments: two
// questions, as a model asks them.
const migrationQuestions = `{"questions":[` +
	`{"id":"strategy","header":"Migration","question":"How should the users table change?","options":[` +
	`{"label":"Expand and contract (Recommended)","description":"Add the column, backfill, drop the old one later."},` +
	`{"label":"Rename in place","description":"One migration, with a short lock on the table."}]},` +
	`{"id":"rollout","header":"Rollout","question":"When should it run?","options":[` +
	`{"label":"Next deploy","description":"Ships with the code."},` +
	`{"label":"By hand","description":"You run it in a quiet hour."}]}]}`

func askCall(args string) fakellm.Reply {
	return fakellm.Reply{Calls: []fakellm.Call{{Name: engine.QuestionToolName, Args: args}}}
}

// openAsking opens a session on an engine that offers request_user_input.
func (e *env) openAsking(t *testing.T, interactive bool) (*session.Session, *events) {
	t.Helper()
	eng := embedded.New(embedded.Config{StateDir: e.StateDir, Provider: "openai", Getenv: e.getenv, AskUser: true})
	s, err := session.Open(context.Background(), eng, session.Options{Settings: e.settings(), Interactive: interactive})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	return s, &events{t: t, s: s}
}

// TestQuestions_Offered: the tool is offered with Codex's name,
// description, and schema only when the engine's sessions have a user.
func TestQuestions_Offered(t *testing.T) {
	e := newEnv(t, fakellm.Reply{Text: "done"}, fakellm.Reply{Text: "done"})
	s, ev := e.openAsking(t, true)
	_, err := s.Submit("hi")
	require.NoError(t, err)
	ev.finished()

	plain, pev := e.open(t, e.embedded(), "")
	_, err = plain.Submit("hi")
	require.NoError(t, err)
	pev.finished()

	reqs := e.llm.Requests()
	require.Len(t, reqs, 2)
	assert.Contains(t, reqs[0].ToolNames, engine.QuestionToolName)
	assert.Equal(t, engine.QuestionToolName, reqs[0].ToolNames[len(reqs[0].ToolNames)-1], "after the other tools")
	assert.NotContains(t, reqs[1].ToolNames, engine.QuestionToolName)

	var schema struct {
		Required   []string
		Properties struct {
			Questions struct {
				Description string
				Items       struct {
					Required   []string
					Properties map[string]struct {
						Description string
						Items       struct{ Required []string }
					}
				}
			}
		}
	}
	require.NoError(t, json.Unmarshal([]byte(reqs[0].Tools[engine.QuestionToolName]), &schema))
	assert.Equal(t, []string{"questions"}, schema.Required)
	q := schema.Properties.Questions
	assert.Equal(t, "Questions to show the user. Prefer 1 and do not exceed 3", q.Description)
	assert.Equal(t, []string{"id", "header", "question", "options"}, q.Items.Required)
	assert.Equal(t, "Short header label shown in the UI (12 or fewer chars).", q.Items.Properties["header"].Description)
	assert.Contains(t, q.Items.Properties["options"].Description, `the client will add a free-form "Other" option automatically.`)
	assert.Equal(t, []string{"label", "description"}, q.Items.Properties["options"].Items.Required)

	description := ""
	for _, raw := range reqs[0].ToolDefs {
		var def struct{ Name, Description string }
		require.NoError(t, json.Unmarshal(raw, &def))
		if def.Name == engine.QuestionToolName {
			description = def.Description
		}
	}
	assert.Equal(t, "Request user input for one to three short questions and wait for the response.", description)
}

// TestQuestions_AnswerReturnsToTheModel: the call waits for the user, as
// long as it takes, and the answers go back as the call's result in
// Codex's encoding; the transcript, live and loaded, gets the answers.
func TestQuestions_AnswerReturnsToTheModel(t *testing.T) {
	e := newEnv(t, askCall(migrationQuestions), fakellm.Reply{Text: "Going with expand and contract."})
	s, ev := e.openAsking(t, true)
	_, err := s.Submit("plan the migration")
	require.NoError(t, err)

	asked := ev.until("QuestionsAsked", isA[session.QuestionsAsked]).(session.QuestionsAsked)
	require.Len(t, asked.Questions, 2)
	assert.Equal(t, "Migration", asked.Questions[0].Header)
	assert.Equal(t, "Expand and contract (Recommended)", asked.Questions[0].Options[0].Label)
	assert.NotEmpty(t, asked.CallID)

	time.Sleep(200 * time.Millisecond)
	assert.Len(t, e.llm.Requests(), 1, "the model waits with the user")

	answers := engine.Answers{
		"strategy": {Answers: []string{"Expand and contract (Recommended)"}},
		"rollout":  {Answers: []string{engine.OtherAnswer, engine.NotePrefix + "after the backup on Friday"}},
	}
	require.NoError(t, s.AnswerQuestions(asked.ID, answers))
	answered := ev.until("QuestionsAnswered", isA[session.QuestionsAnswered]).(session.QuestionsAnswered)
	assert.False(t, answered.Canceled)
	recorded := ev.until("engine.QuestionsAnswered", isA[engine.QuestionsAnswered]).(engine.QuestionsAnswered)
	assert.Equal(t, asked.CallID, recorded.CallID)
	assert.Equal(t, answers, recorded.Answers)
	assert.Equal(t, asked.Questions, recorded.Questions)
	result := ev.finished()
	assert.Equal(t, "Going with expand and contract.", result.Answer)

	reqs := e.llm.Requests()
	require.Len(t, reqs, 2)
	assert.JSONEq(t, `{"answers":{"rollout":{"answers":["None of the above","user_note: after the backup on Friday"]},"strategy":{"answers":["Expand and contract (Recommended)"]}}}`,
		reqs[1].ToolOutputs[0])

	runs, err := session.Load(e.StateDir, s.ID())
	require.NoError(t, err)
	var loaded []engine.QuestionsAnswered
	for _, run := range runs {
		for _, x := range run.Events {
			if q, ok := x.(engine.QuestionsAnswered); ok {
				loaded = append(loaded, q)
			}
		}
	}
	require.Len(t, loaded, 1)
	assert.Equal(t, answers, loaded[0].Answers)
}

// TestQuestions_InterruptCancels: an interrupt ends the wait, the call
// fails with Codex's message, and the next run carries it to the model.
func TestQuestions_InterruptCancels(t *testing.T) {
	e := newEnv(t, askCall(migrationQuestions), fakellm.Reply{Text: "ok"})
	s, ev := e.openAsking(t, true)
	_, err := s.Submit("plan the migration")
	require.NoError(t, err)
	ev.until("QuestionsAsked", isA[session.QuestionsAsked])

	require.NoError(t, s.Interrupt())
	answered := ev.until("QuestionsAnswered", isA[session.QuestionsAnswered]).(session.QuestionsAnswered)
	assert.True(t, answered.Canceled)
	assert.Equal(t, core.StatusInterrupted, ev.finished().Status)
	ev.idle()

	_, err = s.Submit("just use expand and contract")
	require.NoError(t, err)
	ev.finished()
	reqs := e.llm.Requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, []string{"request_user_input was cancelled before receiving a response"}, reqs[1].ToolOutputs)
	assert.Equal(t, []string{"plan the migration", "just use expand and contract"}, reqs[1].UserTexts)
}

// TestQuestions_Refused: a session no user reads refuses the call, and so
// do questions without options, as Codex does.
func TestQuestions_Refused(t *testing.T) {
	noOptions := `{"questions":[{"id":"a","header":"A","question":"Which?","options":[]}]}`
	e := newEnv(t, askCall(migrationQuestions), askCall(noOptions), fakellm.Reply{Text: "done"})
	s, ev := e.openAsking(t, false)
	_, err := s.Submit("go")
	require.NoError(t, err)
	ev.finished()

	reqs := e.llm.Requests()
	require.Len(t, reqs, 3)
	assert.Contains(t, reqs[0].ToolNames, engine.QuestionToolName)
	assert.Equal(t, []string{"request_user_input is not available: no user can answer in this session; ask in your final message instead"}, reqs[1].ToolOutputs)
	assert.Equal(t, 0, countKind[session.QuestionsAsked](ev.all))
	assert.Contains(t, reqs[2].ToolOutputs[1], "request_user_input is not available")
}

// TestQuestions_NeedOptions: questions without options are refused with
// Codex's message before the user sees them.
func TestQuestions_NeedOptions(t *testing.T) {
	noOptions := `{"questions":[{"id":"a","header":"A","question":"Which?","options":[]}]}`
	e := newEnv(t, askCall(noOptions), fakellm.Reply{Text: "done"})
	s, ev := e.openAsking(t, true)
	_, err := s.Submit("go")
	require.NoError(t, err)
	ev.finished()

	reqs := e.llm.Requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, []string{"request_user_input requires non-empty options for every question"}, reqs[1].ToolOutputs)
	assert.Equal(t, 0, countKind[session.QuestionsAsked](ev.all))
}

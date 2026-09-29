package agents_test

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/agents"
	"github.com/viktordanov/uagent-harness/internal/codereview"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/engine/embedded"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// reviewAnswer is a reviewer's answer in Codex's format.
const reviewAnswer = `{"findings":[{"title":"[P1] Check the error","body":"It is dropped.","confidence_score":0.8,"priority":1,
"code_location":{"absolute_file_path":"/w/a.go","line_range":{"start":3,"end":4}}}],
"overall_correctness":"patch is incorrect","overall_explanation":"One bug.","overall_confidence_score":0.7}`

// uncommitted is the start of Codex's prompt for the uncommitted changes.
const uncommitted = "Review the current code changes"

func (ev *events) reviewFinished() session.ReviewFinished {
	ev.t.Helper()

	return ev.until("ReviewFinished", func(x core.Event) bool { _, ok := x.(session.ReviewFinished); return ok }).(session.ReviewFinished)
}

func reviewerRequests(e *env) []fakellm.Request {
	return slices.DeleteFunc(e.llm.Requests(), func(r fakellm.Request) bool {
		return !slices.ContainsFunc(r.UserTexts, func(s string) bool { return strings.HasPrefix(s, uncommitted) })
	})
}

// TestReview_ReadOnlySubagent runs /review end to end: the reviewer is a
// fresh session with Codex's rubric as its system prompt, the review
// model, Bash and ViewImage only, and the read-only sandbox, whose writes
// fail and whose escalations are declined without asking. Its findings
// come back parsed, and reach the main agent with the next message in
// Codex's <user_action>.
func TestReview_ReadOnlySubagent(t *testing.T) {
	e := newEnv(t, agents.Config{ReviewModel: "gpt-review"}, fakellm.Reply{Text: "I will fix it"})
	e.llm.Route(uncommitted,
		fakellm.Reply{Commands: []string{"touch written.txt", "echo seen"}},
		fakellm.Reply{Escalated: []string{"touch escalated.txt"}},
		fakellm.Reply{Text: reviewAnswer},
	)
	s, ev := e.open(t, true, e.sandboxed(t))

	require.NoError(t, s.Review(context.Background(), codereview.Target{Kind: codereview.Uncommitted}))
	fin := ev.reviewFinished()

	require.Empty(t, fin.Err)
	assert.False(t, fin.Interrupted)
	require.Len(t, fin.Output.Findings, 1)
	assert.Equal(t, "[P1] Check the error", fin.Output.Findings[0].Title)
	assert.Equal(t, "patch is incorrect", fin.Output.OverallCorrectness)

	started := slices.IndexFunc(ev.all, func(x core.Event) bool { r, ok := x.(session.ReviewStarted); return ok && r.Hint == "current changes" })
	assert.GreaterOrEqual(t, started, 0, "ReviewStarted says what is reviewed")
	activity := 0
	for _, x := range ev.all {
		_, asked := x.(session.ApprovalRequested)
		assert.False(t, asked, "the reviewer never asks for approval")
		if a, ok := x.(session.ReviewActivity); ok && a.ID == fin.ID {
			activity++
		}
		_, agent := x.(engine.AgentUpdated)
		assert.False(t, agent, "the reviewer is not one of the agent's subagents")
	}
	assert.Positive(t, activity, "the reviewer's tool calls are reported")

	reqs := reviewerRequests(e)
	require.NotEmpty(t, reqs)
	for _, r := range reqs {
		assert.True(t, strings.HasSuffix(strings.TrimSpace(r.System), strings.TrimSpace(codereview.Instructions())), "Codex's rubric replaces the host prompt")
		assert.NotContains(t, r.System, "apply_patch", "none of the main agent's instructions")
		assert.Equal(t, "gpt-review", r.Model)
		assert.Equal(t, []string{"Bash", "ViewImage"}, r.ToolNames, "no apply_patch, MCP, or agent tools")
	}
	outputs := strings.Join(reqs[len(reqs)-1].ToolOutputs, "\n")
	assert.Contains(t, outputs, "seen")
	assert.Regexp(t, `(?i)not permitted|read-only file system`, outputs, "the write failed in the sandbox")
	assert.Contains(t, outputs, "this session never asks for approval", "the escalation was declined")
	assert.NoFileExists(t, filepath.Join(e.Workspace, "written.txt"), "the sandbox is read-only")
	assert.NoFileExists(t, filepath.Join(e.Workspace, "escalated.txt"), "an escalation is declined")
	assert.Len(t, e.llm.Requests(), len(reqs), "the main agent was not asked")

	_, err := s.Submit("fix the finding")
	require.NoError(t, err)
	assert.Equal(t, "I will fix it", ev.finished().Answer)
	main := e.llm.Requests()[len(e.llm.Requests())-1]
	require.Len(t, main.UserTexts, 2)
	assert.Equal(t, codereview.ExitMessage(fin.Output, false), main.UserTexts[0], "Codex's hand-over goes first")
	assert.Equal(t, "fix the finding", main.UserTexts[1])

	infos, err := session.Sessions(e.StateDir)
	require.NoError(t, err)
	i := slices.IndexFunc(infos, func(in session.Info) bool { return in.ID != s.ID() })
	require.GreaterOrEqual(t, i, 0)
	assert.Equal(t, s.ID(), infos[i].Parent, "the reviewer's session is a child of the main one")
}

// TestReview_Interrupted stops a review with the session's interrupt:
// the main agent gets Codex's interrupted form.
func TestReview_Interrupted(t *testing.T) {
	gate := make(chan struct{})
	t.Cleanup(func() { close(gate) })
	e := newEnv(t, agents.Config{}, fakellm.Reply{Text: "ok"})
	e.llm.Route(uncommitted, fakellm.Reply{Gate: gate, Text: reviewAnswer})
	s, ev := e.open(t, false)

	done := make(chan error, 1)
	go func() { done <- s.Review(context.Background(), codereview.Target{Kind: codereview.Uncommitted}) }()
	ev.until("ReviewStarted", func(x core.Event) bool { _, ok := x.(session.ReviewStarted); return ok })
	<-e.llm.Seen() // the reviewer's request is out
	require.NoError(t, s.Interrupt())
	fin := ev.reviewFinished()
	require.NoError(t, <-done)

	assert.True(t, fin.Interrupted)
	assert.Empty(t, fin.Output.Findings)
	_, err := s.Submit("next")
	require.NoError(t, err)
	ev.finished()
	main := e.llm.Requests()[len(e.llm.Requests())-1]
	assert.Contains(t, main.UserTexts[0], "User initiated a review task, but was interrupted.")
}

// TestReview_OneAtATime refuses a second review while one runs, and a
// review on an engine without a reviewer.
func TestReview_OneAtATime(t *testing.T) {
	gate := make(chan struct{})
	e := newEnv(t, agents.Config{})
	e.llm.Route(uncommitted, fakellm.Reply{Gate: gate, Text: reviewAnswer})
	s, ev := e.open(t, false)

	done := make(chan error, 1)
	go func() { done <- s.Review(context.Background(), codereview.Target{Kind: codereview.Uncommitted}) }()
	ev.until("ReviewStarted", func(x core.Event) bool { _, ok := x.(session.ReviewStarted); return ok })
	err := s.Review(context.Background(), codereview.Target{Kind: codereview.Custom, Instructions: "again"})
	require.ErrorContains(t, err, "a review is already running")
	close(gate)
	require.NoError(t, <-done)
	assert.Len(t, ev.reviewFinished().Output.Findings, 1)

	bare, _ := e.open(t, false, func(c *embedded.Config) { c.Subagents = nil })
	require.ErrorIs(t, bare.Review(context.Background(), codereview.Target{Kind: codereview.Uncommitted}), session.ErrNoReview)
}

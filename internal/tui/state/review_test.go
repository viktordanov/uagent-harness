package state_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/codereview"
	"github.com/viktordanov/uah/internal/gitdiff"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/internal/tui/state"
)

var targets = state.ReviewTargetsLoaded{
	Branches: gitdiff.Branches{Names: []string{"main", "feature", "fix-login"}, Current: "feature"},
	Commits: []gitdiff.Commit{
		{SHA: "aaaaaaa1111111", Subject: "Add the parser"},
		{SHA: "bbbbbbb2222222", Subject: "Fix the login"},
	},
}

func TestDiff_Command(t *testing.T) {
	_, effects := apply(opened(), state.Submit{Text: "/diff"})
	assert.Equal(t, []state.Effect{state.EffDiff{Dir: "/workspace"}}, effects)

	t.Run("changes become an item the agent never sees", func(t *testing.T) {
		d := gitdiff.Diff{Root: "/workspace", Files: []gitdiff.File{{Untracked: true}}}
		s, effects := apply(opened(), state.DiffShown{Diff: d})
		assert.Empty(t, effects)
		require.Equal(t, []state.Kind{state.KindDiff}, kinds(s))
		assert.Equal(t, &d, s.Items[0].GitDiff)
	})

	notices := map[string]state.DiffShown{
		"/diff — not inside a git repository": {Err: gitdiff.ErrNotRepo},
		"No changes detected.":                {},
		"Failed to compute diff: boom":        {Err: errors.New("boom")},
	}
	for text, shown := range notices {
		s, _ := apply(opened(), shown)
		require.Len(t, s.Items, 1)
		assert.Equal(t, text, s.Items[0].Text)
	}
}

// TestReview_Menu is Codex's review popup as the command menu: the
// presets, then the branches or the commits, loaded once.
func TestReview_Menu(t *testing.T) {
	s := opened()
	assert.Equal(t, []string{"branch", "uncommitted", "commit", "<instructions>"}, labels(s.Suggestions("/review ")))
	assert.Equal(t, []string{"uncommitted"}, labels(s.Suggestions("/review un")))
	assert.Empty(t, s.Suggestions("/review check the error handling"), "instructions have no menu, so enter sends them")

	s, effects := apply(s, state.DraftChanged{Draft: "/review "})
	assert.Equal(t, []state.Effect{state.EffLoadReviewTargets{Dir: "/workspace"}}, effects)
	_, effects = apply(s, state.DraftChanged{Draft: "/review b"})
	assert.Empty(t, effects, "one load at a time")
	assert.Empty(t, s.Suggestions("/review branch "), "nothing until the lists arrive")

	s, _ = apply(s, targets)
	branches := s.Suggestions("/review branch ")
	assert.Equal(t, []string{"main", "feature", "fix-login"}, labels(branches))
	assert.Equal(t, "checked out", branches[1].Help)
	assert.Equal(t, []string{"feature", "fix-login"}, labels(s.Suggestions("/review branch f")))

	commits := s.Suggestions("/review commit ")
	assert.Equal(t, []string{"Add the parser", "Fix the login"}, labels(commits))
	assert.Equal(t, "aaaaaaa", commits[0].Help)
	assert.Equal(t, "/review commit aaaaaaa1111111", commits[0].Draft)
	assert.Equal(t, []string{"Fix the login"}, labels(s.Suggestions("/review commit login")))
	assert.Equal(t, []string{"Fix the login"}, labels(s.Suggestions("/review commit bbb")))

	t.Run("accepting a preset opens its list", func(t *testing.T) {
		_, effects := apply(s, state.MenuEnter{Draft: "/review "})
		assert.Equal(t, []state.Effect{state.EffSetDraft{Text: "/review branch "}}, effects)
	})
}

func TestReview_Targets(t *testing.T) {
	s, _ := apply(opened(), targets)
	cases := map[string]codereview.Target{
		"/review uncommitted":             {Kind: codereview.Uncommitted},
		"/review branch main":             {Kind: codereview.BaseBranch, Branch: "main"},
		"/review commit bbbbbbb":          {Kind: codereview.Commit, SHA: "bbbbbbb2222222", Title: "Fix the login"},
		"/review commit 1234abc":          {Kind: codereview.Commit, SHA: "1234abc"},
		"/review look at the error paths": {Kind: codereview.Custom, Instructions: "look at the error paths"},
	}
	for text, want := range cases {
		got, effects := apply(s, state.Submit{Text: text})
		assert.Equal(t, []state.Effect{state.EffReview{Target: want}}, effects, text)
		assert.Nil(t, got.Menu.Review, "the lists are read again for the next review")
	}

	for _, text := range []string{"/review", "/review branch", "/review commit"} {
		_, effects := apply(s, state.Submit{Text: text})
		require.NotEmpty(t, effects, text)
		assert.IsType(t, state.EffSetDraft{}, effects[0], "an unfinished target goes back to the menu")
	}

	busy, _ := apply(s, session.InputQueued{At: t0, Input: core.UserInput{ID: "m1", Text: "work"}})
	_, effects := apply(busy, state.Submit{Text: "/review uncommitted"})
	assert.Empty(t, effects, "as in Codex, /review waits until the agent is idle")
}

// TestReview_Item follows a review from its start to its findings; esc
// esc stops it, and a second review waits.
func TestReview_Item(t *testing.T) {
	s, _ := apply(opened(), session.ReviewStarted{At: t0, ID: "r1", Hint: "current changes"})
	require.Equal(t, []state.Kind{state.KindReview}, kinds(s))
	it := s.Items[0]
	assert.True(t, it.Live())
	assert.Equal(t, "current changes", it.Review.Hint)
	assert.True(t, s.ReviewRunning())

	s, _ = apply(s, session.ReviewActivity{At: t0, ID: "r1", Event: core.ToolCalled{At: t0, CallID: "c1", Name: "Bash", Label: `{"command":"git diff"}`}})
	assert.Equal(t, "git diff", s.Items[0].Review.Doing)

	_, effects := apply(s, state.Submit{Text: "/review uncommitted"})
	assert.Empty(t, effects, "one review at a time")

	armed, _ := apply(s, state.Esc{})
	_, effects = apply(armed, state.Esc{})
	assert.Equal(t, []state.Effect{state.EffInterrupt{}}, effects, "esc esc stops the review")

	out := codereview.Parse(`{"findings":[{"title":"[P1] x","body":"y","code_location":{"absolute_file_path":"/workspace/a.go","line_range":{"start":1,"end":2}}}]}`)
	s, _ = apply(s, session.ReviewFinished{At: t0.Add(time.Minute), ID: "r1", Output: out})
	it = s.Items[0]
	assert.False(t, it.Live())
	assert.False(t, s.ReviewRunning())
	assert.Equal(t, out, it.Review.Output)
	assert.Empty(t, it.Review.Doing)
	assert.Equal(t, "/workspace", it.Review.Workspace)
}

// TestReview_HandOverIsANote shows the message that gave the review to
// the main agent as a line, not as the user's message.
func TestReview_HandOverIsANote(t *testing.T) {
	text := codereview.ExitMessage(codereview.Output{OverallExplanation: "Fine."}, false)
	s, _ := apply(opened(), core.UserMessage{At: t0, ID: "r1", Text: text})
	require.Equal(t, []state.Kind{state.KindNotice}, kinds(s))
	assert.Equal(t, "the review went to the main agent with this message", s.Items[0].Text)
}

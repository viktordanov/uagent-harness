package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/images"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// reduceAll is apply with the history file's appends kept.
func reduceAll(s state.State, evs ...any) (state.State, []state.Effect) {
	var all []state.Effect
	for _, ev := range evs {
		var effects []state.Effect
		s, effects = state.Reduce(s, ev)
		all = append(all, effects...)
	}

	return s, all
}

func withPrompts(texts ...string) state.State {
	s, _ := apply(opened(), state.PromptsLoaded{Texts: texts})

	return s
}

func draftOf(t *testing.T, effects []state.Effect) string {
	t.Helper()
	require.Len(t, effects, 1)
	d, ok := effects[0].(state.EffSetDraft)
	require.True(t, ok, "%#v", effects[0])

	return d.Text
}

func TestHistory_UpAndDownOnAnEmptyComposer(t *testing.T) {
	assert.False(t, opened().Recalls("", true, false), "no history, no recall: ↑ does what it did")

	s := withPrompts("one", "two", "three")
	require.True(t, s.Recalls("", false, false))
	assert.False(t, s.Recalls("", false, true), "↓ recalls only while browsing")

	s, effects := apply(s, state.RecallOlder{})
	assert.Equal(t, "three", draftOf(t, effects))
	s, effects = apply(s, state.RecallOlder{}, state.RecallOlder{})
	require.Len(t, effects, 2)
	assert.Equal(t, state.EffSetDraft{Text: "one"}, effects[1])
	s, effects = apply(s, state.RecallOlder{})
	assert.Empty(t, effects, "the oldest stays")

	s, effects = apply(s, state.RecallNewer{})
	assert.Equal(t, "two", draftOf(t, effects))
	s, _ = apply(s, state.RecallNewer{})
	s, effects = apply(s, state.RecallNewer{})
	assert.Equal(t, "", draftOf(t, effects), "past the newest: the empty draft again")
	assert.False(t, s.Recalls("", true, true), "and browsing ended")
	_, effects = apply(s, state.RecallOlder{})
	assert.Equal(t, "three", draftOf(t, effects), "↑ starts at the newest again")
}

// TestHistory_AnEditedPromptKeepsTheArrows: ↑ and ↓ recall only while the
// composer holds the recalled prompt unchanged and the cursor is at its
// start or end, as Codex's should_handle_navigation.
func TestHistory_AnEditedPromptKeepsTheArrows(t *testing.T) {
	s, _ := apply(withPrompts("one", "two\nlines"), state.RecallOlder{})
	assert.True(t, s.Recalls("two\nlines", true, false), "at an edge of the recalled prompt")
	assert.True(t, s.Recalls("two\nlines", true, true))
	assert.False(t, s.Recalls("two\nlines", false, false), "inside it, the cursor moves")
	assert.False(t, s.Recalls("two\nlines!", true, false), "edited, the cursor moves")
	assert.False(t, s.Recalls("draft", true, false), "a draft of your own is never replaced")
	assert.True(t, s.Recalls("", false, false), "emptied, ↑ recalls again")
}

func TestHistory_SentPromptsAreRecordedAndRecalled(t *testing.T) {
	s, effects := reduceAll(withPrompts("from the file"), state.Submit{Text: "  fix the tests  "})
	assert.Contains(t, effects, state.EffRecordPrompt{SessionID: "sess-1", Text: "fix the tests"})

	s, effects = reduceAll(s, state.Steer{Text: "fix the tests"}, state.Submit{Text: "/model gpt-6-luna"})
	assert.Contains(t, effects, state.EffRecordPrompt{SessionID: "sess-1", Text: "fix the tests"}, "the file gets each prompt, as Codex's")
	assert.NotContains(t, effects, state.EffRecordPrompt{SessionID: "sess-1", Text: "/model gpt-6-luna"}, "slash commands stay out of the file")

	s, effects = apply(s, state.RecallOlder{})
	assert.Equal(t, "/model gpt-6-luna", draftOf(t, effects), "but ↑ recalls them in this process")
	assert.Empty(t, s.Suggestions("/model gpt-6-luna"), "a recalled command opens no menu")
	s, effects = apply(s, state.RecallOlder{})
	assert.Equal(t, "fix the tests", draftOf(t, effects))
	_, effects = apply(s, state.RecallOlder{})
	assert.Equal(t, "from the file", draftOf(t, effects), "a repeat collapses into one entry")
}

func TestHistory_TheFileLoadsBeforeThisProcesssPrompts(t *testing.T) {
	s, _ := apply(opened(), state.Submit{Text: "early"}, state.PromptsLoaded{Texts: []string{"old"}})
	s, effects := apply(s, state.RecallOlder{})
	assert.Equal(t, "early", draftOf(t, effects))
	_, effects = apply(s, state.RecallOlder{})
	assert.Equal(t, "old", draftOf(t, effects))
}

func TestHistory_ShellCommands(t *testing.T) {
	s, effects := reduceAll(opened(), state.EnterShell{}, state.Submit{Text: "go test ./..."})
	assert.Contains(t, effects, state.EffRecordPrompt{SessionID: "sess-1", Text: "!go test ./..."}, "with its !, as Codex's")
	require.False(t, s.Shell)

	s, effects = apply(s, state.RecallOlder{})
	assert.Equal(t, "go test ./...", draftOf(t, effects))
	assert.True(t, s.Shell, "a recalled command comes back in shell mode")
	assert.True(t, s.Recalls("go test ./...", true, true), "and ↓ goes on from it")

	s, effects = apply(s, state.RecallNewer{})
	assert.Equal(t, "", draftOf(t, effects))
	assert.False(t, s.Shell, "past the newest, shell mode ends")
}

func TestHistory_ImagesComeBackInThisProcess(t *testing.T) {
	s, _ := apply(withImages(), state.ImageAttached{Image: stored("a")})
	s, effects := reduceAll(s, state.Submit{Text: "see [Image #1]"})
	assert.Contains(t, effects, state.EffRecordPrompt{SessionID: "sess-1", Text: "see [Image #1]"}, "the file keeps the placeholder, as Codex's")
	require.Empty(t, s.Attached)

	s, effects = apply(s, state.RecallOlder{})
	assert.Equal(t, "see [Image #1]", draftOf(t, effects))
	require.Len(t, s.Attached, 1, "the image is attached again")
	assert.Equal(t, "[Image #1]", s.Attached[0].Label)
}

// TestHistory_CtrlCKeepsTheDraft: a draft ctrl+c clears comes back with ↑,
// as in Codex, and never reaches the file.
func TestHistory_CtrlCKeepsTheDraft(t *testing.T) {
	s, effects := reduceAll(withPrompts("old"), state.DraftCleared{Draft: "half a thought"})
	assert.Empty(t, effects)
	_, effects = apply(s, state.RecallOlder{})
	assert.Equal(t, "half a thought", draftOf(t, effects))
}

func TestHistory_ReverseSearch(t *testing.T) {
	s := withPrompts("git status", "Fix the build", "run the tests", "fix the build", "git status")
	s.Shell = true // the draft of your own is a command
	s, effects := apply(s, state.SearchOpen{Draft: "ls"})
	assert.Empty(t, effects, "opening previews nothing, as in Codex")
	require.NotNil(t, s.History.Search)
	assert.Empty(t, s.Suggestions("/x"), "no menu while searching")

	s, effects = apply(s, state.SearchType{Text: "F"}, state.SearchType{Text: "i"})
	assert.Equal(t, state.EffSetDraft{Text: "fix the build"}, effects[len(effects)-1], "the newest match, ignoring case")
	assert.Equal(t, state.SearchMatch, s.History.Search.Status)
	assert.False(t, s.Shell)

	s, effects = apply(s, state.SearchOpen{})
	assert.Equal(t, "Fix the build", draftOf(t, effects), "ctrl+r again: the next older text")
	s, effects = apply(s, state.SearchMove{Older: true})
	assert.Empty(t, effects, "at the oldest match it stays")
	assert.Equal(t, state.SearchMatch, s.History.Search.Status)
	s, effects = apply(s, state.SearchMove{})
	assert.Equal(t, "fix the build", draftOf(t, effects))

	s, effects = apply(s, state.SearchType{Text: "z"})
	assert.Equal(t, "ls", draftOf(t, effects), "no match shows your draft")
	assert.Equal(t, state.SearchNoMatch, s.History.Search.Status)
	assert.True(t, s.Shell)
	s, _ = apply(s, state.SearchAccept{})
	require.NotNil(t, s.History.Search, "enter keeps only a match")

	s, effects = apply(s, state.SearchType{Text: "\b"}, state.SearchClear{})
	assert.Equal(t, "ls", draftOf(t, effects[len(effects)-1:]))
	assert.Equal(t, state.SearchIdle, s.History.Search.Status)

	s, _ = apply(s, state.SearchType{Text: "git"})
	s, effects = apply(s, state.SearchCancel{})
	assert.Equal(t, "ls", draftOf(t, effects), "esc puts the draft back")
	assert.True(t, s.Shell)
	assert.Nil(t, s.History.Search)
}

func TestHistory_AcceptingAMatchGoesOnFromIt(t *testing.T) {
	s := withPrompts("alpha", "beta", "gamma")
	s, _ = apply(s, state.SearchOpen{}, state.SearchType{Text: "bet"})
	s, effects := apply(s, state.SearchAccept{})
	assert.Empty(t, effects, "the match is already the draft")
	assert.Nil(t, s.History.Search)
	require.True(t, s.Recalls("beta", true, false))
	_, effects = apply(s, state.RecallOlder{})
	assert.Equal(t, "alpha", draftOf(t, effects))
}

func TestHistory_SearchFindsImagesByTheirPlaceholder(t *testing.T) {
	img := stored("a")
	img.Label = images.Label(1)
	s := withPrompts(images.Join("see [Image #1]", []images.Image{img}))
	s, effects := apply(s, state.SearchOpen{}, state.SearchType{Text: "image #1"})
	assert.Equal(t, "see [Image #1]", draftOf(t, effects))
	assert.Len(t, s.Attached, 1)
}

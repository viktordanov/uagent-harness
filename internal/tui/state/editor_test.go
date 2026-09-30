package state_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/internal/tui/state"
)

// TestEditor_OpensTheDraft: ctrl+g hands the draft, placeholders as text,
// to the shell's editor, also while the agent works; the picker and
// /config take no ctrl+g.
func TestEditor_OpensTheDraft(t *testing.T) {
	s, _ := apply(opened(), state.ImageAttached{Image: stored("a")})
	s.Busy = true
	_, effects := apply(s, state.EditDraft{Draft: "look at [Image #1]\nand this"})
	assert.Equal(t, []state.Effect{state.EffEditDraft{Text: "look at [Image #1]\nand this"}}, effects)

	picker, _ := apply(opened(), state.SessionsLoaded{})
	require.Equal(t, state.ModePicker, picker.Mode)
	_, effects = apply(picker, state.EditDraft{Draft: "x"})
	assert.Empty(t, effects)

	config, _ := apply(opened(), state.Submit{Text: "/config"})
	require.NotNil(t, config.Config)
	_, effects = apply(config, state.EditDraft{Draft: "x"})
	assert.Empty(t, effects)
}

// TestEditor_TheSavedTextIsTheDraft: the saved text replaces the draft
// without the editor's final newline and with \n line ends; images whose
// placeholder stayed are kept, deleted ones drop, and a placeholder typed
// in the editor attaches nothing.
func TestEditor_TheSavedTextIsTheDraft(t *testing.T) {
	s, _ := apply(opened(), state.ImageAttached{Image: stored("a")}, state.ImageAttached{Image: stored("b")})

	s, effects := apply(s, state.DraftEdited{Text: "keep [Image #2]\r\nand [Image #7]\r\n\r\n"})
	assert.Equal(t, []state.Effect{state.EffSetDraft{Text: "keep [Image #2]\nand [Image #7]\n"}}, effects,
		"one trailing newline goes; a blank line the user left stays")
	require.Len(t, s.Attached, 1)
	assert.Equal(t, "[Image #2]", s.Attached[0].Label)

	_, effects = apply(s, state.Submit{Text: "keep [Image #2]\nand [Image #7]"})
	sent := effects[0].(state.EffSubmit).Text
	assert.Contains(t, sent, stored("b").Ref)
	assert.NotContains(t, sent, stored("a").Ref)
}

// TestEditor_MultiLineRoundTrip: a draft saved unchanged, as vim writes it
// with a final newline, comes back unchanged, including its own trailing
// spaces and inner blank lines.
func TestEditor_MultiLineRoundTrip(t *testing.T) {
	draft := "first line  \n\n\tindented\n```go\nfunc f() {}\n```"
	_, effects := apply(opened(), state.DraftEdited{Text: draft + "\n"})
	assert.Equal(t, []state.Effect{state.EffSetDraft{Text: draft}}, effects)
}

// TestEditor_EmptyResultEmptiesTheComposer: saving an empty file leaves an
// empty composer and drops the draft's images, as Claude Code does.
func TestEditor_EmptyResultEmptiesTheComposer(t *testing.T) {
	s, _ := apply(opened(), state.ImageAttached{Image: stored("a")})
	s, effects := apply(s, state.DraftEdited{Text: ""})
	assert.Equal(t, []state.Effect{state.EffSetDraft{Text: ""}}, effects)
	assert.Empty(t, s.Attached)
}

// TestEditor_AFailureKeepsTheDraft: an editor that exits non-zero or does
// not start leaves the draft and its images, and says so.
func TestEditor_AFailureKeepsTheDraft(t *testing.T) {
	s, _ := apply(opened(), state.ImageAttached{Image: stored("a")})
	s, effects := apply(s, state.DraftEdited{Err: errors.New("vim: exit status 1")})
	assert.Empty(t, effects, "the composer keeps its text")
	require.Len(t, s.Attached, 1)
	last := s.Items[len(s.Items)-1]
	assert.Equal(t, session.LevelWarning, last.Level)
	assert.Equal(t, "editor: vim: exit status 1; the draft is unchanged", last.Text)
}

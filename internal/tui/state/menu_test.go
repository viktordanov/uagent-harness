package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

func labels(items []state.Suggestion) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Label)
	}

	return out
}

func TestMenu_Commands(t *testing.T) {
	s := opened()
	assert.Equal(t, []string{"/resume [id]", "/rewind", "/reasoning"}, labels(s.Suggestions("/re")))
	assert.Equal(t, []string{"low", "medium"}, labels(s.Suggestions("/effort ")[:2]))
	assert.Equal(t, []string{"medium", "max"}, labels(s.Suggestions("/effort m")))
	assert.Empty(t, s.Suggestions("hello"), "plain text has no menu")
	assert.Empty(t, s.Suggestions("/effort low"), "a complete value has no menu")
}

func TestMenu_MoveAcceptClose(t *testing.T) {
	s := opened()
	s, _ = apply(s, state.MenuMove{Draft: "/re", Delta: 1})
	assert.Equal(t, 1, s.Menu.Index)
	s, effects := apply(s, state.MenuAccept{Draft: "/re"})
	assert.Equal(t, []state.Effect{state.EffSetDraft{Text: "/rewind"}}, effects)
	assert.Equal(t, 0, s.Menu.Index)

	s, _ = apply(s, state.MenuMove{Draft: "/re", Delta: -1})
	assert.Equal(t, 2, s.Menu.Index, "moving up from the top wraps")
	s, _ = apply(s, state.MenuClose{Draft: "/re"})
	assert.False(t, s.MenuOpen("/re"))
	assert.True(t, s.MenuOpen("/res"), "a new draft opens it again")
}

func TestMenu_Mentions(t *testing.T) {
	s := opened()
	s, effects := apply(s, state.DraftChanged{Draft: "look at @"})
	require.Equal(t, []state.Effect{state.EffLoadFiles{}}, effects, "the first @ loads the file list")
	_, effects = apply(s, state.DraftChanged{Draft: "look at @x"})
	assert.Empty(t, effects, "only once")

	s, _ = apply(s, state.FilesLoaded{Paths: []string{"README.md", "internal/session/session.go", "cmd/uah/main.go"}})
	items := s.Suggestions("look at @sesgo")
	require.NotEmpty(t, items)
	assert.Equal(t, "internal/session/session.go", items[0].Label)
	assert.Equal(t, "look at internal/session/session.go ", items[0].Draft)
}

func TestMenu_EnterRunsOrFills(t *testing.T) {
	s, effects := apply(opened(), state.MenuEnter{Draft: "/deta"})
	assert.Equal(t, []state.Effect{state.EffSetDraft{Text: ""}}, effects)
	assert.True(t, s.Details, "a command without arguments runs")

	_, effects = apply(opened(), state.MenuEnter{Draft: "/eff"})
	assert.Equal(t, []state.Effect{state.EffSetDraft{Text: "/effort "}}, effects, "one with arguments is filled in")
}

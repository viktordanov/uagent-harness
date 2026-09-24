package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/contextusage"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

func TestReduce_ContextView(t *testing.T) {
	s, effects := apply(opened(), state.Submit{Text: "/context"})
	assert.Equal(t, []state.Effect{state.EffContext{}}, effects)

	s, _ = apply(s, state.ContextShown{})
	assert.Contains(t, s.Items[len(s.Items)-1].Text, "no context to show yet")

	u := contextusage.Usage{Model: "gpt-5.5", Window: 1000, Used: 100}
	s, _ = apply(s, state.ContextShown{Usage: u, OK: true})
	last := s.Items[len(s.Items)-1]
	require.Equal(t, state.KindContext, last.Kind)
	assert.Equal(t, &u, last.Context)
}

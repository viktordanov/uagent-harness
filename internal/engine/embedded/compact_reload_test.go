package embedded_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

func TestEmbedded_ReloadedTranscriptShowsCompactions(t *testing.T) {
	e := newEnv(t, fakellm.Reply{Commands: []string{"echo one"}}, fakellm.Reply{Text: "answer one"},
		fakellm.Reply{Text: "SUMMARY"}, fakellm.Reply{Text: "answer two"})
	s, ev := e.open(t, e.compacting(0), "")
	ask(t, s, ev, "first")
	require.NoError(t, s.Compact())
	ask(t, s, ev, "second")

	runs, err := session.Load(e.StateDir, s.ID())
	require.NoError(t, err)
	require.Len(t, runs, 2)
	var kinds []string
	for _, x := range runs[1].Events {
		switch v := x.(type) {
		case core.UserMessage:
			kinds = append(kinds, "user "+v.Text)
		case engine.Compacted:
			kinds = append(kinds, "compacted "+v.Summary)
		case core.AssistantMessage:
			kinds = append(kinds, "assistant "+v.Text)
		}
	}
	assert.Equal(t, []string{"user second", "compacted SUMMARY", "assistant answer two"}, kinds,
		"the compaction sits between the message and the answer: %s", strings.Join(kinds, ", "))
}

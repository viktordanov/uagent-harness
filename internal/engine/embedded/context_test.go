package embedded_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/contextusage"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

func TestEmbedded_ContextUsageBreaksDownTheLastRequest(t *testing.T) {
	e := newEnv(t,
		fakellm.Reply{Commands: []string{"echo one"}, InputTokens: 9_000},
		fakellm.Reply{Text: "done", InputTokens: 12_000},
	)
	s, ev := e.open(t, e.compacting(90), "")
	_, ok := s.ContextUsage()
	assert.False(t, ok, "no request yet")

	ask(t, s, ev, "first")
	u, ok := s.ContextUsage()
	require.True(t, ok)
	assert.Equal(t, int64(12_000), u.Used, "the reported input tokens of the last request")
	assert.False(t, u.Estimated)
	assert.Positive(t, u.Buffer)
	names := map[string]int64{}
	var sum int64
	for _, c := range u.Categories {
		names[c.Name] = c.Tokens
		sum += c.Tokens
	}
	assert.Equal(t, u.Used, sum)
	for _, c := range []string{contextusage.SystemPrompt, contextusage.Tools, contextusage.UserMessages, contextusage.ToolResults} {
		assert.Positive(t, names[c], c)
	}
}

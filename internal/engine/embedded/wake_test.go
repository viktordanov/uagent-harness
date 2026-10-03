package embedded_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/testing/fakellm"
)

// TestWakeWaitsForEveryCallOfTheTurn: a quick command's result does not
// wake the model while the turn's slow one runs; both arrive together.
func TestWakeWaitsForEveryCallOfTheTurn(t *testing.T) {
	t.Parallel()
	e := newEnv(t,
		fakellm.Reply{Commands: []string{"echo fast", "sleep 2; echo slow"}},
		fakellm.Reply{Text: "done"},
	)
	s, ev := e.open(t, e.embedded(), "")
	_, err := s.Submit("run both")
	require.NoError(t, err)
	ev.finished()

	reqs := e.llm.Requests()
	require.Len(t, reqs, 2)
	outputs := strings.Join(reqs[1].ToolOutputs, "\n")
	assert.Contains(t, outputs, "fast")
	assert.Contains(t, outputs, "slow")
}

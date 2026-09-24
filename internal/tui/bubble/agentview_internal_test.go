package bubble

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"
)

// TestAgentView_EndedWatchDrains keeps reading an ended watch's batches
// until they close, so its batch goroutine never blocks on a send.
func TestAgentView_EndedWatchDrains(t *testing.T) {
	m := Model{watchGen: 2}
	batches := make(chan []core.Event, 1)
	_, cmd := m.onAgentEvents(agentEventsMsg{gen: 1, id: "a", batches: batches})
	require.NotNil(t, cmd, "a stale batch still reads the next one")

	batches <- []core.Event{core.AssistantMessage{Text: "late"}}
	msg := cmd()
	assert.Equal(t, 1, msg.(agentEventsMsg).gen, "the drain keeps the old generation")

	close(batches)
	_, cmd = m.onAgentEvents(msg.(agentEventsMsg))
	ended := cmd()
	assert.Equal(t, agentWatchEndedMsg{gen: 1, id: "a"}, ended)
	_, cmd = m.onAgentWatchEnded(ended.(agentWatchEndedMsg))
	assert.Nil(t, cmd, "an ended watch that is not shown is not reopened")
}

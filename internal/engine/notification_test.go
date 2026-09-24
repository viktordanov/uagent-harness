package engine_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/engine"
)

type raw string

func (r raw) MarshalJSON() ([]byte, error) { return []byte(r), nil }

// TestSubagentNotification writes and reads Codex's notification, with a
// status that is a string or an object.
func TestSubagentNotification(t *testing.T) {
	for status, want := range map[string]string{
		`"interrupted"`:             "interrupted",
		`{"completed":"forty-two"}`: "completed",
		`{"errored":"model gone"}`:  "errored",
	} {
		note, err := engine.SubagentNotification("subagent-1", raw(status))
		require.NoError(t, err)
		assert.Contains(t, note, "<subagent_notification>\n")
		assert.True(t, json.Valid([]byte(note[len("<subagent_notification>\n"):len(note)-len("\n</subagent_notification>")])))
		id, state, ok := engine.ParseSubagentNotification(note)
		assert.True(t, ok)
		assert.Equal(t, "subagent-1", id)
		assert.Equal(t, want, state)
	}
	_, _, ok := engine.ParseSubagentNotification("an ordinary message")
	assert.False(t, ok)
}

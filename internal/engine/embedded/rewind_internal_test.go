package embedded

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/unreallabsai/unreal-agent/harness/inbox"
	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/session"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore"
)

func input(t *testing.T, id, text string) sessionstore.Item {
	t.Helper()
	payload, err := json.Marshal(text)
	require.NoError(t, err)

	return sessionstore.Item{Kind: sessionstore.ItemInput, Data: inbox.Input{ID: inbox.ID(id), Kind: inbox.InputExternal, Payload: payload}}
}

func turn(id string) sessionstore.Item {
	return sessionstore.Item{Kind: sessionstore.ItemTurn, Data: session.Turn{ID: session.TurnID(id)}}
}

func response(turnID string, calls ...string) sessionstore.Item {
	out := []llm.Item{{Type: llm.ItemMessage, Data: llm.Message{Role: llm.RoleAssistant, Text: "ok"}}}
	for _, c := range calls {
		out = append(out, llm.Item{Type: llm.ItemToolCall, Data: llm.ToolCall{CallID: c, Name: "Bash"}})
	}

	return sessionstore.Item{Kind: sessionstore.ItemModelResponse, Data: sessionstore.ModelResponse{TurnID: session.TurnID(turnID), Response: llm.Response{Output: out}}}
}

func status(turnID, call string) sessionstore.Item {
	return sessionstore.Item{Kind: sessionstore.ItemToolCallStatus, Data: sessionstore.ToolCallStatus{TurnID: session.TurnID(turnID), CallID: call}}
}

func TestRewindPoint_TakesTheInputsThatWentWithTheMessage(t *testing.T) {
	items := []sessionstore.Item{
		input(t, "m1", "first"), turn("t1"), response("t1", "c1"), status("t1", "c1"), turn("t2"), response("t2"),
		input(t, "note", "a note"), input(t, "q1", "queued"), input(t, "m2", "second"), turn("t3"), response("t3"),
	}
	from, held, err := rewindPoint(items, "m2")
	require.NoError(t, err)
	assert.Equal(t, 6, from, "the batch starts at the note")
	assert.Equal(t, []string{"a note", "queued"}, held)

	from, held, err = rewindPoint(items, "m1")
	require.NoError(t, err)
	assert.Zero(t, from)
	assert.Empty(t, held)
}

func TestRewindPoint_RefusesAMessageSentWhileAToolCallWaited(t *testing.T) {
	items := []sessionstore.Item{
		input(t, "m1", "first"), turn("t1"), response("t1", "c1"),
		input(t, "steer", "also this"), status("t1", "c1"), turn("t2"), response("t2"),
	}
	_, _, err := rewindPoint(items, "steer")
	require.ErrorIs(t, err, errMidTool)

	_, _, err = rewindPoint(items, "missing")
	require.ErrorIs(t, err, errNotInContext)
}

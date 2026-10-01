package embedded

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/unreallabsai/unreal-agent/harness/operation"
	"github.com/unreallabsai/unreal-agent/harness/session"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore"
)

// recordingStore lists the writes that reach it.
type recordingStore struct {
	sessionstore.Store

	writes []string
}

func (r *recordingStore) SaveOperation(_ context.Context, _ session.ID, v operation.Operation) error {
	r.writes = append(r.writes, "op "+string(v.ID)+" "+string(v.Status)+map[bool]string{true: " state", false: ""}[v.State != nil])

	return nil
}

func (r *recordingStore) AppendToolCallStatus(_ context.Context, _ session.ID, s sessionstore.ToolCallStatus) error {
	r.writes = append(r.writes, "status "+s.CallID)

	return nil
}

func (r *recordingStore) AppendTurn(context.Context, session.ID, session.Turn) error {
	r.writes = append(r.writes, "turn")

	return nil
}

// TestCheckpointStore_HoldsTerminalStates: a terminal state waits; the
// status that carries it goes first and the state follows without its
// body. Anything else, and the run's end, writes it whole first.
func TestCheckpointStore_HoldsTerminalStates(t *testing.T) {
	rec := &recordingStore{}
	c := &checkpointStore{Store: rec}
	ctx, state := t.Context(), []byte(`{"Result":{"Out":"big"}}`)
	op := func(id string, s operation.Status) operation.Operation {
		return operation.Operation{ID: operation.ID(id), Type: "remote_job", Version: 1, Status: s, State: state}
	}

	require.NoError(t, c.SaveOperation(ctx, "s", op("a", operation.StatusCompleted)))
	require.NoError(t, c.SaveOperation(ctx, "s", op("b", operation.StatusFailed)))
	assert.Empty(t, rec.writes, "held")
	require.NoError(t, c.AppendToolCallStatus(ctx, "s", sessionstore.ToolCallStatus{CallID: "call-a", Operations: []operation.Operation{op("a", operation.StatusCompleted)}}))
	require.NoError(t, c.AppendTurn(ctx, "s", session.Turn{}))
	require.NoError(t, c.SaveOperation(ctx, "s", op("c", operation.StatusCanceled)))
	require.NoError(t, c.SaveOperation(ctx, "s", op("d", operation.StatusAwaiting)))
	require.NoError(t, c.SaveOperation(ctx, "s", op("e", operation.StatusCompleted)))
	require.NoError(t, c.AppendToolCallStatus(ctx, "s", sessionstore.ToolCallStatus{CallID: "call-e", Operations: []operation.Operation{op("e", operation.StatusAwaiting)}}))
	require.NoError(t, c.flush(ctx))

	assert.Equal(t, []string{
		"op b failed state", "status call-a", "op a completed",
		"turn",
		"op c canceled state", "op d awaiting state",
		"op e completed state", "status call-e", // the status has another state
	}, rec.writes)
}

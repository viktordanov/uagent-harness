package embedded

import (
	"encoding/json/jsontext"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/unreallabsai/unreal-agent/harness/operation"
	"github.com/unreallabsai/unreal-agent/harness/tool"
)

// A call found awaiting after a restart fails instead of running twice.
func TestMCPJobs_InterruptedCallIsNotRepeated(t *testing.T) {
	jobs := newMCPJobs(t.Context(), nil)
	spec, err := operation.NewRemoteJobSpec(operation.RemoteJobPlan{
		Type: mcpPlanType, Version: mcpPlanVersion, Data: jsontext.Value(`{"server":"s","tool":"t","arguments":{}}`),
	})
	require.NoError(t, err)
	op := operation.Operation{ID: "op-1", Type: spec.Type, Version: spec.Version, MaxOutputLength: spec.MaxOutputLength, State: spec.State, Status: operation.StatusAwaiting}

	require.NoError(t, jobs.AddRemoteJob(op))
	select {
	case got := <-jobs.RemoteJobUpdates():
		assert.Equal(t, operation.StatusFailed, got.Status)
		state, err := operation.DecodeRemoteJobState(got)
		require.NoError(t, err)
		assert.Contains(t, state.TerminalError, "interrupted")
		result, err := mcpTranslator{name: "mcp__s__t"}.TranslateResult("call-1", tool.CallStatus{}, []operation.Operation{got})
		require.NoError(t, err)
		assert.Contains(t, result.Output[0].Value, "Error: interrupted")
	case <-time.After(5 * time.Second):
		t.Fatal("no update")
	}
}

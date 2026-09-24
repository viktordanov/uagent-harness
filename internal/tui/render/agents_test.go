package render_test

import (
	"testing"

	"github.com/viktordanov/uagent-harness/internal/engine"
)

func TestScreen_Agents(t *testing.T) {
	s := apply(base(), engine.AgentUpdated{At: t0, ID: "a1", Nickname: "Ada", State: engine.AgentRunning, Started: t0})
	s = apply(s, engine.AgentUpdated{At: t0, ID: "a2", Nickname: "Rex", Role: "reviewer", State: engine.AgentCompleted, Started: t0})
	golden(t, "agents", screen(s, ""))
}

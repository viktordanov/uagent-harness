package render_test

import (
	"testing"
	"time"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

func TestScreen_Agents(t *testing.T) {
	s := apply(base(), engine.AgentUpdated{At: t0, ID: "a1", Nickname: "Ada", State: engine.AgentRunning, Started: t0})
	s = apply(s, engine.AgentUpdated{At: t0, ID: "a2", Nickname: "Rex", Role: "reviewer", State: engine.AgentCompleted, Started: t0})
	s = apply(s,
		engine.AgentActivity{At: t0, ID: "a2", Event: core.ToolCalled{At: t0, CallID: "c1", Name: "Bash", Label: "go test ./..."}},
		engine.AgentActivity{At: t0, ID: "a2", Event: core.ToolFinished{At: t0, CallID: "c1", Name: "Bash", OK: true, Detail: "exit 0", Duration: 2 * time.Second}},
	)
	golden(t, "agents", screen(s, ""))
	golden(t, "agents-details", screen(apply(s, state.ToggleDetails{}), ""))
}

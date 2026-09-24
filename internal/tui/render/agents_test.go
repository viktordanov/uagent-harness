package render_test

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
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

// TestScreen_AgentView draws the viewed agent's transcript under one header
// line, instead of the session's.
func TestScreen_AgentView(t *testing.T) {
	s := apply(base(), engine.AgentUpdated{At: t0, ID: "a1", Nickname: "Ada", State: engine.AgentRunning, Started: t0})
	s = apply(s, core.AssistantMessage{At: t0, Text: "the parent's answer", Final: true})
	s = apply(s, state.AgentViewOpened{ID: "a1", Nickname: "Ada", Events: []core.Event{
		session.SessionOpened{At: t0, ID: "a1", Settings: session.Settings{Provider: "openai", Model: "gpt-child", Workspace: "/w"}},
		core.AssistantMessage{At: t0, Text: "the child's answer", Final: true},
	}})
	out := ansi.Strip(screen(s, ""))
	lines := strings.Split(out, "\n")
	assert.Equal(t, " agent Ada · alt+← alt+→ switch agents · esc esc interrupts", strings.TrimRight(lines[0], " "))
	assert.Contains(t, out, "the child's answer")
	assert.NotContains(t, out, "the parent's answer")
	assert.Contains(t, out, "gpt-child", "the footer is the agent's")
}

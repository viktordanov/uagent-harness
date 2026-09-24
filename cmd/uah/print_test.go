package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
)

// TestPrinter_Agents prints a subagent's state, and its tool calls with
// --verbose.
func TestPrinter_Agents(t *testing.T) {
	var out bytes.Buffer
	p := newPrinter(&out, true)
	p.print(engine.AgentUpdated{ID: "a1", Nickname: "Ada", State: engine.AgentRunning})
	p.print(engine.AgentActivity{ID: "a1", Event: core.ToolFinished{Name: "Bash", Label: "ls", OK: true, Detail: "exit 0", Duration: time.Second}})

	assert.Contains(t, out.String(), "agent Ada: running")
	assert.Contains(t, out.String(), "agent Ada: Bash  ls (exit 0, 1.0s)")

	out.Reset()
	quiet := newPrinter(&out, false)
	quiet.print(engine.AgentActivity{ID: "a1", Event: core.ToolFinished{Name: "Bash"}})
	assert.Empty(t, out.String())
}

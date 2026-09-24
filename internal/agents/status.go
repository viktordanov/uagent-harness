package agents

import (
	"encoding/json"

	"github.com/unreallabsai/unreal-agent/harness/operation"

	"github.com/viktordanov/uagent-harness/internal/engine"
)

// resultBudget bounds the final messages in one tool result, in
// characters, below the runner's cap on a remote job's result (40,000), so
// the runner never cuts the JSON.
const resultBudget = 36_000

// Status is a child's state; Message is a completed child's final answer
// or an errored child's error.
type Status struct {
	State   string
	Message string
}

// Final reports whether the child has stopped working: wait returns it.
// An interrupted child is final here, unlike in Codex, because nothing
// but the parent's send_input starts it again.
func (s Status) Final() bool {
	return s.State != engine.AgentRunning && s.State != engine.AgentPendingInit
}

// MarshalJSON encodes the status as Codex's AgentStatus: a string, or
// {"completed": message} (null without one) and {"errored": message}.
func (s Status) MarshalJSON() ([]byte, error) {
	var v any = s.State
	switch s.State {
	case engine.AgentCompleted:
		var message *string
		if s.Message != "" {
			message = &s.Message
		}
		v = map[string]*string{"completed": message}
	case engine.AgentErrored:
		v = map[string]string{"errored": s.Message}
	}

	return json.Marshal(v) //nolint:wrapcheck // plain values
}

// bound shares resultBudget among the statuses' messages, keeping the head
// and the tail of a long one, as the runner bounds tool output.
func bound(statuses map[string]Status) map[string]Status {
	if len(statuses) == 0 {
		return statuses
	}
	limit := resultBudget / len(statuses)
	out := make(map[string]Status, len(statuses))
	for id, s := range statuses {
		s.Message, _ = operation.BoundOutput(s.Message, limit)
		out[id] = s
	}

	return out
}

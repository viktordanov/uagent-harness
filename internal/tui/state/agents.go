package state

import (
	"fmt"
	"strings"
	"time"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// onAgentUpdated shows a subagent as one line, added where it was spawned
// and updated in place: KindAgent with Name the nickname, Label the role,
// and Detail its state.
func (s *State) onAgentUpdated(e engine.AgentUpdated) {
	key := "agent:" + e.ID
	set := func(it *Item) { it.Detail, it.Started = e.State, e.Started }
	if s.update(key, set) {
		return
	}
	it := Item{Kind: KindAgent, Key: key, Name: e.Nickname, Label: e.Role, Text: e.ID}
	set(&it)
	s.put(it)
}

// cmdAgents lists the session's subagents with their state.
func cmdAgents(s *State, _ string) []Effect {
	var b strings.Builder
	for _, it := range s.Items {
		if it.Kind != KindAgent {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s", it.Name)
		if it.Label != "" {
			fmt.Fprintf(&b, " (%s)", it.Label)
		}
		fmt.Fprintf(&b, " · %s", it.Detail)
		if it.Detail == engine.AgentRunning {
			fmt.Fprintf(&b, " %s", time.Since(it.Started).Round(time.Second))
		}
		fmt.Fprintf(&b, " · %s", it.Text)
	}
	if b.Len() == 0 {
		s.notice(session.LevelInfo, "no subagents in this session; the agent starts them with spawn_agent when you ask it to delegate")

		return nil
	}
	s.notice(session.LevelInfo, b.String())

	return nil
}

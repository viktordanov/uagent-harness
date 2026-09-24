package state

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// The agent tools' calls read as names, not JSON: "WAIT  Ada, Rex",
// "SPAWN  Ada · gpt-6-luna low · Summarize…".

// agentTargets are the agent tools that name existing subagents.
var agentTargets = map[string]bool{
	"wait_agent": true, "wait": true, "close_agent": true, "send_input": true, "resume_agent": true,
}

// agentCallLabel is the label of an agent tool call that names subagents:
// their nicknames, then the message a send_input carries.
func (s *State) agentCallLabel(name, label string) string {
	if !agentTargets[name] {
		return label
	}
	var args struct {
		Targets []string `json:"targets"`
		Target  string   `json:"target"`
		ID      string   `json:"id"`
		Message string   `json:"message"`
	}
	if json.Unmarshal([]byte(label), &args) != nil {
		return label
	}
	ids := args.Targets
	for _, id := range []string{args.Target, args.ID} {
		if id != "" {
			ids = append(ids, id)
		}
	}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, s.agentName(id))
	}
	out := strings.Join(names, ", ")
	if args.Message != "" {
		out += " · " + args.Message
	}

	return out
}

// agentName is a subagent's nickname, else its short ID.
func (s *State) agentName(id string) string {
	if i, ok := s.index["agent:"+id]; ok && s.Items[i].Name != "" {
		return s.Items[i].Name
	}

	return session.ShortID(id)
}

// labelSpawn names the spawn_agent call that started a subagent once the
// subagent reports: its nickname, model and effort, and task.
func (s *State) labelSpawn(e engine.AgentUpdated) {
	if e.CallID == "" {
		return
	}
	parts := []string{e.Nickname}
	if m := strings.TrimSpace(e.Model + " " + e.Effort); m != "" {
		parts = append(parts, m)
	}
	if e.Forked {
		parts = append(parts, "forked")
	}
	if e.Task != "" {
		parts = append(parts, e.Task)
	}
	s.update("call:"+e.CallID, func(it *Item) { it.Label = strings.Join(parts, " · ") })
}

// settle keeps how a subagent ended when it is closed afterwards: a
// completed or failed child that close_agent shuts down still reads done
// or failed.
func settle(prev, next string) string {
	if next == engine.AgentShutdown && (prev == engine.AgentCompleted || prev == engine.AgentErrored) {
		return prev
	}

	return next
}

// agentNote reads Codex's <subagent_notification>, which a subagent's end
// sends the main agent as a message, as a line of the transcript:
// "Ada completed; the main agent was told".
func (s *State) agentNote(text string) (string, bool) {
	body, ok := strings.CutPrefix(strings.TrimSpace(text), "<subagent_notification>")
	if !ok {
		return "", false
	}
	body, _ = strings.CutSuffix(body, "</subagent_notification>")
	var n struct {
		AgentPath string          `json:"agent_path"`
		Status    json.RawMessage `json:"status"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(body)), &n) != nil {
		return "", false
	}
	state := strings.Trim(string(n.Status), `"`)
	var obj map[string]json.RawMessage
	if json.Unmarshal(n.Status, &obj) == nil {
		for k := range obj {
			state = k
		}
	}

	return fmt.Sprintf("%s %s; the main agent was told", s.agentName(n.AgentPath), state), true
}

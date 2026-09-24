package engine

import (
	"encoding/json"
	"strings"
)

// Codex's <subagent_notification>: the user-role message that tells a
// parent's agent a subagent reached a final status (Codex
// core/src/context/subagent_notification.rs). internal/agents writes it and
// the TUI reads it, both through these two functions.
const (
	notificationOpen  = "<subagent_notification>"
	notificationClose = "</subagent_notification>"
)

// SubagentNotification is the message for the agent's final status, in
// Codex's status encoding ("interrupted", {"completed": "…"}, …).
func SubagentNotification(agentID string, status json.Marshaler) (string, error) {
	body, err := json.Marshal(struct {
		AgentPath string         `json:"agent_path"`
		Status    json.Marshaler `json:"status"`
	}{agentID, status})
	if err != nil {
		return "", err //nolint:wrapcheck // a plain encoding
	}

	return notificationOpen + "\n" + string(body) + "\n" + notificationClose, nil
}

// ParseSubagentNotification reads such a message: the agent's ID and its
// state's name (completed, errored, interrupted, …). ok is false for any
// other text.
func ParseSubagentNotification(text string) (agentID, state string, ok bool) {
	body, found := strings.CutPrefix(strings.TrimSpace(text), notificationOpen)
	if !found {
		return "", "", false
	}
	body, _ = strings.CutSuffix(body, notificationClose)
	var n struct {
		AgentPath string          `json:"agent_path"`
		Status    json.RawMessage `json:"status"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(body)), &n) != nil {
		return "", "", false
	}
	if json.Unmarshal(n.Status, &state) == nil {
		return n.AgentPath, state, true
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(n.Status, &obj) == nil {
		for k := range obj {
			return n.AgentPath, k, true
		}
	}

	return n.AgentPath, "", true
}

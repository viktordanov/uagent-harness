package embedded

import (
	"fmt"
	"strings"

	"github.com/viktordanov/uagent-harness/internal/engine"
)

// The spawn_agent guidance is adapted from openai/codex
// (codex-rs/core/src/tools/handlers/multi_agents_spec.rs), Copyright 2025
// OpenAI, licensed under the Apache License, Version 2.0
// (http://www.apache.org/licenses/LICENSE-2.0). Changes: the tool names
// are uah's (wait instead of wait_agent), and the fork and upload wording
// is dropped because children share the parent's workspace.
const spawnGuidance = `Spawn a sub-agent for a well-scoped task. Returns the spawned agent's id and nickname at once; the agent works in the background in your workspace, with your sandbox and approvals. Spawned agents inherit your current model by default. Do not set the ` + "`model`" + ` field unless the user explicitly asks for a different model.

Do not spawn sub-agents unless the user or applicable AGENTS.md/skill instructions explicitly ask for sub-agents, delegation, or parallel agent work.
Requests for depth, thoroughness, research, investigation, or detailed codebase analysis do not count as permission to spawn.
Agent-role guidance below only helps choose which agent to use after spawning is already authorized; it never authorizes spawning by itself.

### When to delegate vs. do the subtask yourself
- First, quickly analyze the overall user task and form a succinct high-level plan. Identify which tasks are immediate blockers on the critical path, and which tasks are sidecar tasks that are needed but can run in parallel without blocking the next local step. Decide what immediate task you should do locally right now before delegating.
- Use a subagent when a subtask is easy enough for it to handle and can run in parallel with your local work. Prefer delegating concrete, bounded sidecar tasks that materially advance the main task without blocking your immediate next local step.
- Do not delegate urgent blocking work when your immediate next step depends on that result.
- Keep work local when the subtask is too difficult to delegate well and when it is tightly coupled, urgent, or likely to block your immediate next step.

### Designing delegated subtasks
- Subtasks must be concrete, well-defined, and self-contained, and must materially advance the main task.
- Do not duplicate work between yourself and delegated subtasks.
- Narrow the delegated ask to the concrete output you need next.
- Agents share your workspace: for code-edit subtasks, give each delegated task a disjoint write set, and ask the agent to list the file paths it changed in its final answer.

### After you delegate
- Call wait very sparingly. Only call wait when you need the result immediately for the next critical-path step and you are blocked until it returns.
- Do not redo delegated subagent tasks yourself; focus on integrating results or tackling non-overlapping work.
- While the subagent is running in the background, do meaningful non-overlapping work immediately.
- Do not repeatedly wait by reflex.
- Close agents with close_agent when you no longer need them; open agents count toward the limit.`

// spawnDescription is the spawn_agent description: the roles, then the
// guidance.
func spawnDescription(roles []engine.AgentRole) string {
	if len(roles) == 0 {
		return spawnGuidance
	}
	var b strings.Builder
	b.WriteString("Available agent types (agent_type):\n")
	for _, r := range roles {
		fmt.Fprintf(&b, "- `%s`: %s\n", r.Name, oneLine(r.Description))
	}
	b.WriteString("\n")
	b.WriteString(spawnGuidance)

	return b.String()
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// The agent tools' parameter schemas.
const (
	spawnSchema = `{"type":"object","properties":{` +
		`"message":{"type":"string","description":"Initial plain-text task for the new agent."},` +
		`"agent_type":{"type":"string","description":"Agent type for the new agent, from the list above. Omit for the default agent."},` +
		`"model":{"type":"string","description":"Model override for the new agent. Omit unless an explicit override is needed."},` +
		`"reasoning_effort":{"type":"string","enum":["low","medium","high","xhigh","max"],"description":"Reasoning effort override for the new agent. Omit to inherit your effort."}` +
		`},"required":["message"],"additionalProperties":false}`
	sendSchema = `{"type":"object","properties":{` +
		`"id":{"type":"string","description":"Agent id to message (from spawn_agent)."},` +
		`"message":{"type":"string","description":"Message to send to the agent."}` +
		`},"required":["id","message"],"additionalProperties":false}`
	waitSchema = `{"type":"object","properties":{` +
		`"ids":{"type":"array","items":{"type":"string"},"description":"Agent ids to wait on. Pass multiple ids to wait for whichever finishes first."},` +
		`"timeout_ms":{"type":"number","description":"Timeout in milliseconds. Defaults to 30000, min 10000, max 3600000. Prefer longer waits (minutes) to avoid busy polling."}` +
		`},"required":["ids"],"additionalProperties":false}`
	closeSchema = `{"type":"object","properties":{` +
		`"id":{"type":"string","description":"Agent id to close (from spawn_agent)."}` +
		`},"required":["id"],"additionalProperties":false}`
)

// The other tools' descriptions, after Codex's.
const (
	sendDescription  = "Send a message to an existing agent. A running agent reads it at its next step; a finished one starts working again. Reuse an agent with send_input when the new task depends on the context of its previous one."
	waitDescription  = "Wait for agents to reach a final status. Returns when any listed agent finishes, with each finished agent's status and final message, or an empty status with timed_out when the timeout passes. Other work continues while you wait."
	closeDescription = "Close an agent when it is no longer needed, and return its status before it closed. Finished agents stay open and count toward the concurrency limit until closed."
)

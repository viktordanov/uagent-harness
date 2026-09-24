package agents

import (
	"path/filepath"
	"strings"

	"github.com/viktordanov/uagent-harness/internal/hooks"
)

// maxStopContinuations stops SubagentStop hooks from keeping a child going
// forever, as the session does for Stop hooks.
const maxStopContinuations = 5

// stopCheck is a completed child's SubagentStop check: how many messages
// it had when it finished, and the status it reports unless a hook keeps it
// going.
type stopCheck struct {
	gen    int
	status Status
}

// checkStop runs the SubagentStop hooks for a child that finished. A hook
// that blocks with a reason sends the reason to the child as its next
// message, as Claude Code does; otherwise the child reports its status.
func (m *Manager) checkStop(c *child, check stopCheck) {
	m.mu.Lock()
	in := m.stopInput(c, check.status.Message)
	ctx := c.asks // closing or interrupting the child ends its hooks too
	m.mu.Unlock()
	d := m.template().Hooks.Run(ctx, in)

	m.mu.Lock()
	if c.closed || c.gen != check.gen {
		m.mu.Unlock()

		return // closed, or the parent sent a message meanwhile
	}
	reason := strings.TrimSpace(d.Reason)
	if d.Block && reason != "" && c.stopStreak < maxStopContinuations {
		c.stopStreak++
		m.mu.Unlock()
		if _, err := m.submit(c, reason, false); err == nil {
			return
		}
		m.mu.Lock()
	}
	c.status, c.stopStreak = check.status, 0
	m.mu.Unlock()
	m.notify(c)
}

// stopInput is the SubagentStop payload, Claude Code's: the parent's
// session, and the child's ID, type, transcript, and final answer. It
// holds m.mu.
func (m *Manager) stopInput(c *child, answer string) hooks.Input {
	p := m.parents[c.parent].Request
	in := hooks.Input{
		Event: hooks.SubagentStop, SessionID: c.parent, Cwd: p.Workspace, Model: p.Model,
		StopHookActive: c.stopStreak > 0, AgentID: c.id, AgentType: first(c.role, defaultRole),
		LastAssistantMessage: answer,
	}
	if dir := m.tmpl.SessionsDir; dir != "" {
		in.TranscriptPath = filepath.Join(dir, c.parent+".session.jsonl")
		in.AgentTranscriptPath = filepath.Join(dir, c.id+".session.jsonl")
	}

	return in
}

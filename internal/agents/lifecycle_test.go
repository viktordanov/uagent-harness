package agents_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/agents"
	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// TestAgents_InterruptStopsChildren interrupts the parent: its child's
// live run stops too, and the child stays open as interrupted.
func TestAgents_InterruptStopsChildren(t *testing.T) {
	hold := make(chan struct{}) // never closed: only the interrupt ends these
	e := newEnv(t, agents.Config{},
		fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-I forever"}`)}},
		fakellm.Reply{Gate: hold, Text: "never"},
		fakellm.Reply{Text: "after the interrupt"},
	)
	e.llm.Route("CHILD-I", fakellm.Reply{Gate: hold, Text: "never"})
	s, ev := e.open(t, false)

	_, err := s.Submit("start one")
	require.NoError(t, err)
	running := ev.agentState(engine.AgentRunning)
	require.NoError(t, s.Interrupt())
	assert.Equal(t, core.StatusInterrupted, ev.finished().Status)
	stopped := ev.agentState(engine.AgentInterrupted)
	assert.Equal(t, running.ID, stopped.ID)

	statuses, timedOut, err := e.mgr.Wait(t.Context(), s.ID(), []string{running.ID}, waitTimeout)
	require.NoError(t, err)
	assert.False(t, timedOut)
	assert.Equal(t, engine.AgentInterrupted, statuses[running.ID].State, "an interrupted child is final for wait_agent")
}

// TestAgents_ChildApprovalOutlivesTheParentsRun keeps a child's approval
// open when the parent's run ends, and answers it while the parent is idle.
func TestAgents_ChildApprovalOutlivesTheParentsRun(t *testing.T) {
	parentGate := make(chan struct{})
	e := newEnv(t, agents.Config{},
		fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-J fetch"}`)}},
		fakellm.Reply{Gate: parentGate, Text: "started it"},
	)
	e.llm.Route("CHILD-J", fakellm.Reply{Escalated: []string{"echo fetched"}}, fakellm.Reply{Text: "fetched"})
	s, ev := e.open(t, true, e.sandboxed(t))

	_, err := s.Submit("delegate a fetch")
	require.NoError(t, err)
	req := ev.approval()
	close(parentGate)
	assert.Equal(t, "started it", ev.finished().Answer)
	ev.until("Idle", func(x core.Event) bool { _, ok := x.(session.Idle); return ok })
	require.NoError(t, s.Resolve(req.ID, approval.Approve), "the approval is still open while the parent is idle")
	done := ev.agentState(engine.AgentCompleted)

	assert.Equal(t, "Ada", done.Nickname)
	assert.Contains(t, childOutputs(e, "CHILD-J"), "fetched", "the approved command ran")
	for _, x := range ev.all {
		if r, ok := x.(session.ApprovalResolved); ok {
			assert.Equal(t, approval.Approve, r.Decision, "the parent's run ending did not decline it")
		}
	}
}

// TestAgents_CloseEndsAPendingApproval closes a child that waits for an
// approval: the prompt ends as declined and close_agent returns.
func TestAgents_CloseEndsAPendingApproval(t *testing.T) {
	closeGate := make(chan struct{})
	e := newEnv(t, agents.Config{},
		fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-K fetch"}`)}},
		fakellm.Reply{From: func(req fakellm.Request) fakellm.Reply {
			return fakellm.Reply{Gate: closeGate, Calls: []fakellm.Call{call("close_agent", `{"target":"`+ids(req)[0]+`"}`)}}
		}},
		fakellm.Reply{Text: "closed it"},
	)
	e.llm.Route("CHILD-K", fakellm.Reply{Escalated: []string{"echo fetched"}}, fakellm.Reply{Text: "fetched"})
	s, ev := e.open(t, true, e.sandboxed(t))

	_, err := s.Submit("delegate a fetch")
	require.NoError(t, err)
	req := ev.approval()
	close(closeGate)
	resolved := ev.until("ApprovalResolved", func(x core.Event) bool { _, ok := x.(session.ApprovalResolved); return ok }).(session.ApprovalResolved)
	assert.Equal(t, req.ID, resolved.ID)
	assert.Equal(t, approval.Decline, resolved.Decision)
	assert.Equal(t, "closed it", ev.finished().Answer)
	assert.Contains(t, lastOutputs(e), `{"previous_status":"running"}`)
	ev.agentState(engine.AgentShutdown)
}

// TestAgents_ResumeAcrossProcesses reaches a child of an earlier process:
// send_input says to resume it, resume_agent opens its session with its
// history and nickname, and it answers again.
func TestAgents_ResumeAcrossProcesses(t *testing.T) {
	e := newEnv(t, agents.Config{},
		fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-R first task"}`)}},
		callWith("wait_agent", `{"targets":["ID"]}`),
		fakellm.Reply{Text: "first done"},
		callWith("send_input", `{"target":"ID","message":"second task"}`),
		callWith("resume_agent", `{"id":"ID"}`),
		callWith("send_input", `{"target":"ID","message":"second task"}`),
		callWith("wait_agent", `{"targets":["ID"]}`),
		fakellm.Reply{Text: "second done"},
	)
	e.llm.Route("CHILD-R", fakellm.Reply{Text: "first answer"}, fakellm.Reply{Text: "second answer"})
	s, ev := e.open(t, false)
	_, err := s.Submit("delegate")
	require.NoError(t, err)
	assert.Equal(t, "first done", ev.finished().Answer)
	parentID := s.ID()
	require.NoError(t, s.Close())

	e.mgr = agents.New(e.cfg) // a new process
	s, ev = e.openID(t, parentID, false)
	_, err = s.Submit("again")
	require.NoError(t, err)
	assert.Equal(t, "second done", ev.finished().Answer)

	outputs := lastOutputs(e)
	assert.Contains(t, outputs, "is not loaded; resume it with resume_agent first")
	assert.Contains(t, outputs, `{"status":"pending_init"}`)
	assert.Contains(t, outputs, `{"completed":"second answer"}`)
	var last fakellm.Request
	for _, r := range e.llm.Requests() {
		if isChild(r) {
			last = r
		}
	}
	assert.Equal(t, []string{"CHILD-R first task", "second task"}, last.UserTexts, "the child resumed its own history")
	resumed := ev.agentState(engine.AgentCompleted)
	assert.Equal(t, "Ada", resumed.Nickname, "the child keeps its nickname")
}

// TestAgents_ResumeOnlyOwnChildren refuses to resume a session that is not
// the parent's child.
func TestAgents_ResumeOnlyOwnChildren(t *testing.T) {
	e := newEnv(t, agents.Config{},
		fakellm.Reply{From: func(req fakellm.Request) fakellm.Reply {
			return fakellm.Reply{Calls: []fakellm.Call{call("resume_agent", `{"id":"00000000-0000-0000-0000-000000000000"}`)}}
		}},
		fakellm.Reply{Text: "done"},
	)
	s, ev := e.open(t, false)
	_, err := s.Submit("resume a stranger")
	require.NoError(t, err)
	ev.finished()

	assert.Contains(t, lastOutputs(e), "agent with id 00000000-0000-0000-0000-000000000000 not found")
}

// TestAgents_LongAnswerKeepsTheResultValid bounds a child's very long
// answer so the wait result stays valid JSON under the runner's cap.
func TestAgents_LongAnswerKeepsTheResultValid(t *testing.T) {
	long := strings.Repeat("0123456789", 10_000)
	e := newEnv(t, agents.Config{},
		fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-L write a lot"}`)}},
		callWith("wait_agent", `{"targets":["ID"]}`),
		fakellm.Reply{Text: "read it"},
	)
	e.llm.Route("CHILD-L", fakellm.Reply{Text: long})
	s, ev := e.open(t, false)
	_, err := s.Submit("delegate")
	require.NoError(t, err)
	ev.finished()

	outputs := lastParent(e).ToolOutputs
	result := outputs[len(outputs)-1]
	require.True(t, json.Valid([]byte(result)), "the result is valid JSON")
	assert.Less(t, len(result), 40_000)
	assert.Contains(t, result, "bytes truncated")
	assert.Contains(t, result, "0123456789")
}

// TestAgents_CloseParentClosesChildren closes the parent's session while
// its children run: their runs stop and their sessions close.
func TestAgents_CloseParentClosesChildren(t *testing.T) {
	hold := make(chan struct{})
	e := newEnv(t, agents.Config{},
		fakellm.Reply{Calls: []fakellm.Call{
			call("spawn_agent", `{"message":"CHILD-M one"}`),
			call("spawn_agent", `{"message":"CHILD-M two"}`),
		}},
		fakellm.Reply{Text: "started them"},
	)
	e.llm.Route("CHILD-M", fakellm.Reply{Gate: hold, Text: "never"}, fakellm.Reply{Gate: hold, Text: "never"})
	s, ev := e.open(t, false)
	_, err := s.Submit("start two")
	require.NoError(t, err)
	ev.finished()
	children := ids(lastParent(e))
	require.Len(t, children, 2)

	require.NoError(t, s.Close())
	for _, id := range children {
		statuses, _, err := e.mgr.Wait(t.Context(), s.ID(), []string{id}, waitTimeout)
		require.NoError(t, err)
		assert.Equal(t, engine.AgentShutdown, statuses[id].State)
	}
}

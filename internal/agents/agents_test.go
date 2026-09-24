package agents_test

import (
	"context"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/agents"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/engine/embedded"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
	"github.com/viktordanov/uagent-harness/testing/harnesstest"
)

const waitTimeout = 20 * time.Second

type env struct {
	*harnesstest.Env
	llm *fakellm.Server
	mgr *agents.Manager
}

// newEnv starts one fake model for the parent (the main script) and its
// children (routes), and an embedded engine with the manager.
func newEnv(t *testing.T, cfg agents.Config, replies ...fakellm.Reply) *env {
	t.Helper()
	e := &env{Env: harnesstest.NewEnv(t), llm: fakellm.New(t, replies...)}
	cfg.SessionsDir = filepath.Join(e.StateDir, "sessions")
	if cfg.MaxDepth == 0 {
		cfg.MaxDepth = agents.DefaultMaxDepth
	}
	e.mgr = agents.New(cfg)

	return e
}

func (e *env) getenv(key string) string {
	switch key {
	case "OPENAI_API_KEY":
		return "test-key"
	case "SHELL":
		return "/bin/sh"
	}

	return e.Env.Getenv(key)
}

// open opens the parent session.
func (e *env) open(t *testing.T, interactive bool) (*session.Session, *events) {
	t.Helper()
	eng := embedded.New(embedded.Config{StateDir: e.StateDir, Provider: "openai", Getenv: e.getenv, Subagents: e.mgr})
	e.mgr.Bind(eng)
	s, err := session.Open(context.Background(), eng, session.Options{
		Settings:    session.Settings{Provider: "openai", Model: "gpt-test", Effort: "high", Workspace: e.Workspace, BaseURL: e.llm.URL},
		SessionsDir: filepath.Join(e.StateDir, "sessions"), Source: session.SourceTUI, Interactive: interactive,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	return s, &events{t: t, s: s}
}

type events struct {
	t   *testing.T
	s   *session.Session
	all []core.Event
}

func (ev *events) until(what string, match func(core.Event) bool) core.Event {
	ev.t.Helper()
	deadline := time.After(waitTimeout)
	for {
		select {
		case e, ok := <-ev.s.Events():
			require.True(ev.t, ok, "the session closed while waiting for %s", what)
			ev.all = append(ev.all, e)
			if match(e) {
				return e
			}
		case <-deadline:
			ev.t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func (ev *events) finished() core.Result {
	ev.t.Helper()

	return ev.until("RunFinished", func(e core.Event) bool { _, ok := e.(core.RunFinished); return ok }).(core.RunFinished).Result
}

func call(name, args string) fakellm.Call { return fakellm.Call{Name: name, Args: args} }

var idPattern = regexp.MustCompile(`"id":"([0-9a-f-]{36})"`)

// ids are the agent IDs in the request's tool results, in order.
func ids(req fakellm.Request) []string {
	var out []string
	for _, o := range req.ToolOutputs {
		for _, m := range idPattern.FindAllStringSubmatch(o, -1) {
			if !slices.Contains(out, m[1]) {
				out = append(out, m[1])
			}
		}
	}

	return out
}

// callWith is a reply whose one call uses the first agent ID so far.
func callWith(name, args string) fakellm.Reply {
	return fakellm.Reply{From: func(req fakellm.Request) fakellm.Reply {
		return fakellm.Reply{Calls: []fakellm.Call{call(name, strings.ReplaceAll(args, "ID", ids(req)[0]))}}
	}}
}

// lastParent is the parent's last request: children's messages start
// with CHILD.
func lastParent(e *env) fakellm.Request {
	var last fakellm.Request
	for _, r := range e.llm.Requests() {
		if !slices.ContainsFunc(r.UserTexts, func(s string) bool { return strings.HasPrefix(s, "CHILD") }) {
			last = r
		}
	}

	return last
}

func lastOutputs(e *env) string { return strings.Join(lastParent(e).ToolOutputs, "\n") }

// TestAgents_SpawnWaitAnswer spawns a child, waits for it without holding
// up the parent, and gets its answer; the child cannot spawn and is hidden
// from the resume picker.
func TestAgents_SpawnWaitAnswer(t *testing.T) {
	gate := make(chan struct{})
	var placeholder string
	e := newEnv(t, agents.Config{},
		fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-A count the files"}`)}},
		fakellm.Reply{From: func(req fakellm.Request) fakellm.Reply {
			return fakellm.Reply{Commands: []string{"echo side"}, Calls: []fakellm.Call{call("wait", `{"ids":["`+ids(req)[0]+`"]}`)}}
		}},
		fakellm.Reply{From: func(req fakellm.Request) fakellm.Reply {
			placeholder = strings.Join(req.ToolOutputs, "\n")
			close(gate)

			return fakellm.Reply{Text: "waiting for the agent"}
		}},
		fakellm.Reply{Text: "the agent counted"},
	)
	e.llm.Route("CHILD-A", fakellm.Reply{Gate: gate, Text: "forty-two"})
	s, ev := e.open(t, false)

	_, err := s.Submit("delegate")
	require.NoError(t, err)
	result := ev.finished()

	assert.Equal(t, core.StatusOK, result.Status)
	assert.Equal(t, "the agent counted", result.Answer)
	assert.Contains(t, placeholder, "side", "the command ran while the wait was pending")
	assert.Contains(t, placeholder, "Tool call is still running", "the wait did not hold up the coordinator")
	assert.Contains(t, lastOutputs(e), `"state":"completed","message":"forty-two"`)

	var child fakellm.Request
	for _, r := range e.llm.Requests() {
		if slices.ContainsFunc(r.UserTexts, func(s string) bool { return strings.Contains(s, "CHILD-A") }) {
			child = r
		} else {
			assert.Contains(t, r.Tools, "spawn_agent")
		}
	}
	assert.NotContains(t, child.Tools, "spawn_agent", "depth 1: a child cannot spawn")
	assert.Contains(t, child.Tools, "Bash")

	updates := 0
	for _, x := range ev.all {
		if u, ok := x.(engine.AgentUpdated); ok {
			updates++
			assert.Equal(t, "Ada", u.Nickname)
		}
	}
	assert.Positive(t, updates, "the parent's stream shows the child's progress")

	infos, err := session.Sessions(e.StateDir)
	require.NoError(t, err)
	require.Len(t, infos, 2)
	i := slices.IndexFunc(infos, func(in session.Info) bool { return in.ID != s.ID() })
	assert.Equal(t, session.SourceSubagent, infos[i].Source)
	assert.Equal(t, s.ID(), infos[i].Parent)
	picker := session.Interactive(infos)
	require.Len(t, picker, 1)
	assert.Equal(t, s.ID(), picker[0].ID)
}

// TestAgents_SendInput gives a finished child another task.
func TestAgents_SendInput(t *testing.T) {
	e := newEnv(t, agents.Config{},
		fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-B first task"}`)}},
		callWith("wait", `{"ids":["ID"]}`),
		callWith("send_input", `{"id":"ID","message":"second task"}`),
		callWith("wait", `{"ids":["ID"]}`),
		fakellm.Reply{Text: "both done"},
	)
	e.llm.Route("CHILD-B", fakellm.Reply{Text: "first answer"}, fakellm.Reply{Text: "second answer"})
	s, ev := e.open(t, false)

	_, err := s.Submit("delegate twice")
	require.NoError(t, err)
	result := ev.finished()

	assert.Equal(t, "both done", result.Answer)
	outputs := lastOutputs(e)
	assert.Contains(t, outputs, `"message":"first answer"`)
	assert.Contains(t, outputs, `"status":"sent"`)
	assert.Contains(t, outputs, `"message":"second answer"`)
}

// TestAgents_LimitAndClose refuses a spawn over the limit and accepts it
// once an agent is closed.
func TestAgents_LimitAndClose(t *testing.T) {
	e := newEnv(t, agents.Config{MaxThreads: 1},
		fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-C one"}`)}},
		fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-C two"}`)}},
		callWith("close_agent", `{"id":"ID"}`),
		fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-C three"}`)}},
		fakellm.Reply{Text: "done"},
	)
	e.llm.Route("CHILD-C")
	s, ev := e.open(t, false)

	_, err := s.Submit("spawn three")
	require.NoError(t, err)
	ev.finished()

	outputs := lastOutputs(e)
	assert.Contains(t, outputs, "agent limit reached: 1 agents are open")
	assert.Contains(t, outputs, `"previous_status"`)
	assert.Equal(t, 1, strings.Count(outputs, "agent limit reached"))
	assert.Len(t, ids(lastParent(e)), 2, "the spawn after the close worked")
}

func TestAgents_WaitTimesOut(t *testing.T) {
	m := agents.New(agents.Config{})
	statuses, timedOut, err := m.Wait(context.Background(), "p", []string{"missing"}, time.Millisecond)
	require.NoError(t, err)
	assert.False(t, timedOut)
	assert.Equal(t, engine.AgentNotFound, statuses["missing"].State)
}

package app_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/internal/hooks"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// TestSetup_SubagentHooks runs a session that spawns a subagent, with one
// hook on every event that records its payload. The session events fire
// for the root only; SubagentStart fires before the child's first hook; the
// child's tool hooks carry agent_id and parent_session_id, and the root's
// do not.
func TestSetup_SubagentHooks(t *testing.T) {
	e, in := setupEnv(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	log := filepath.Join(e.StateDir, "hooks.jsonl")
	var cfg strings.Builder
	for _, event := range hooks.Events {
		cfg.WriteString("[[hooks." + string(event) + "]]\ncommand = \"cat >> " + log + " && echo >> " + log + "\"\n")
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(in.ConfigPath), 0o700))
	require.NoError(t, os.WriteFile(in.ConfigPath, []byte(cfg.String()), 0o600))
	llm := fakellm.New(t,
		fakellm.Reply{Calls: []fakellm.Call{{Name: "spawn_agent", Args: `{"message":"CHILD-H check the build"}`}}},
		fakellm.Reply{From: func(req fakellm.Request) fakellm.Reply {
			id := strings.Split(strings.Split(req.ToolOutputs[0], `"agent_id":"`)[1], `"`)[0]
			return fakellm.Reply{Calls: []fakellm.Call{{Name: "wait_agent", Args: `{"targets":["` + id + `"]}`}}}
		}},
		fakellm.Reply{Text: "done"},
	)
	llm.Route("CHILD-H", fakellm.Reply{Commands: []string{"echo built"}}, fakellm.Reply{Text: "the build is fine"})
	in.Provider, in.Model, in.BaseURL = "openai", "gpt-test", llm.URL

	res, err := app.Setup(context.Background(), in, io.Discard)
	require.NoError(t, err)
	opts := res.Options
	opts.Source = session.SourceTUI
	s, err := session.Open(context.Background(), res.Engine, opts)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	_, err = s.Submit("delegate")
	require.NoError(t, err)
	waitFinished(t, s)
	require.NoError(t, s.Close())

	root := s.ID()
	var child string
	for _, id := range sessionIDs(t, e.StateDir) {
		if id != root {
			child = id
		}
	}
	require.NotEmpty(t, child)
	payloads := readPayloads(t, log)

	var rootEvents, childEvents []hooks.Event
	for _, in := range payloads {
		switch {
		case in.SessionID == child:
			childEvents = append(childEvents, in.Event)
			assert.Equal(t, child, in.AgentID, in.Event)
			assert.Equal(t, root, in.ParentSessionID, in.Event)
		case in.Event == hooks.SubagentStart || in.Event == hooks.SubagentStop:
			assert.Equal(t, root, in.SessionID)
			assert.Equal(t, child, in.AgentID)
			assert.Equal(t, "default", in.AgentType)
			assert.Equal(t, filepath.Join(opts.SessionsDir, child+".session.jsonl"), in.AgentTranscriptPath)
			assert.Empty(t, in.ParentSessionID)
			rootEvents = append(rootEvents, in.Event)
		default:
			assert.Equal(t, root, in.SessionID, in.Event)
			assert.Empty(t, in.AgentID, in.Event)
			assert.Empty(t, in.ParentSessionID, in.Event)
			rootEvents = append(rootEvents, in.Event)
		}
	}
	assert.Equal(t, []hooks.Event{hooks.PreToolUse, hooks.PostToolUse}, childEvents, "a child fires its tool hooks only")
	for _, event := range []hooks.Event{hooks.SessionStart, hooks.UserPromptSubmit, hooks.SubagentStart, hooks.SubagentStop, hooks.Stop, hooks.SessionEnd} {
		assert.Contains(t, rootEvents, event)
	}
	started := indexOf(payloads, func(in hooks.Input) bool { return in.Event == hooks.SubagentStart })
	firstChild := indexOf(payloads, func(in hooks.Input) bool { return in.SessionID == child })
	assert.Less(t, started, firstChild, "SubagentStart comes before the child's own hooks")
}

func readPayloads(t *testing.T, path string) []hooks.Input {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var out []hooks.Input
	for line := range strings.Lines(strings.TrimSpace(string(data))) {
		var in hooks.Input
		require.NoError(t, json.Unmarshal([]byte(line), &in), line)
		out = append(out, in)
	}

	return out
}

func indexOf(payloads []hooks.Input, match func(hooks.Input) bool) int {
	for i, in := range payloads {
		if match(in) {
			return i
		}
	}

	return -1
}

package hooks_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/hooks"
)

func run(t *testing.T, in hooks.Input, hs ...hooks.Hook) (hooks.Decision, []hooks.Result) {
	t.Helper()
	trust, err := hooks.LoadTrust(filepath.Join(t.TempDir(), "trust.json"))
	require.NoError(t, err)
	r, err := hooks.New(hs, trust, t.TempDir())
	require.NoError(t, err)
	var results []hooks.Result
	r.OnResult(func(res hooks.Result) { results = append(results, res) })

	return r.Run(context.Background(), in), results
}

func user(event hooks.Event, command string) hooks.Hook {
	return hooks.Hook{Event: event, Command: command, Source: hooks.SourceUser}
}

func TestRun_ExitCodes(t *testing.T) {
	in := hooks.Input{Event: hooks.UserPromptSubmit, Prompt: "hi"}
	cases := map[string]struct {
		command string
		outcome hooks.Outcome
		block   bool
		reason  string
		context []string
	}{
		"exit 0":                  {command: "true", outcome: hooks.OutcomeOK},
		"plain stdout is context": {command: "echo 'the date is today'", outcome: hooks.OutcomeOK, context: []string{"the date is today"}},
		"exit 2 blocks":           {command: "echo 'no secrets' >&2; exit 2", outcome: hooks.OutcomeBlocked, block: true, reason: "no secrets"},
		"exit 1 is ignored":       {command: "echo broken >&2; exit 1", outcome: hooks.OutcomeError, reason: "broken"},
		"JSON block":              {command: `echo '{"decision":"block","reason":"not now"}'`, outcome: hooks.OutcomeOK, block: true, reason: "not now"},
		"JSON context":            {command: `echo '{"hookSpecificOutput":{"additionalContext":"use tabs"}}'`, outcome: hooks.OutcomeOK, context: []string{"use tabs"}},
		"continue false":          {command: `echo '{"continue":false,"stopReason":"halt"}'`, outcome: hooks.OutcomeOK, block: true, reason: "halt"},
		"bad JSON":                {command: `echo '{nope'`, outcome: hooks.OutcomeError},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			d, results := run(t, in, user(hooks.UserPromptSubmit, c.command))
			require.Len(t, results, 1)
			assert.Equal(t, c.outcome, results[0].Outcome)
			assert.Equal(t, c.block, d.Block)
			if c.reason != "" {
				assert.Equal(t, c.reason, firstOf(d.Reason, results[0].Reason))
			}
			assert.Equal(t, c.context, d.Context)
		})
	}
}

func firstOf(a, b string) string {
	if a != "" {
		return a
	}

	return b
}

func TestRun_ReadsTheEventOnStdin(t *testing.T) {
	out := filepath.Join(t.TempDir(), "in.json")
	_, results := run(t, hooks.Input{Event: hooks.PreToolUse, SessionID: "s1", ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"ls"}`)},
		user(hooks.PreToolUse, "cat > "+out))
	require.Len(t, results, 1)
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.JSONEq(t, `{"hook_event_name":"PreToolUse","session_id":"s1","cwd":"","tool_name":"Bash","tool_input":{"command":"ls"}}`, string(data))
}

func TestRun_PreToolUse(t *testing.T) {
	in := hooks.Input{Event: hooks.PreToolUse, ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"rm -rf /"}`)}

	d, _ := run(t, in, user(hooks.PreToolUse, `echo '{"hookSpecificOutput":{"permissionDecision":"deny","permissionDecisionReason":"too dangerous"}}'`))
	assert.True(t, d.Block)
	assert.Equal(t, "too dangerous", d.Reason)

	d, _ = run(t, in, user(hooks.PreToolUse, `echo '{"hookSpecificOutput":{"updatedInput":{"command":"echo safe"}}}'`))
	assert.False(t, d.Block)
	assert.JSONEq(t, `{"command":"echo safe"}`, string(d.UpdatedInput))

	other := user(hooks.PreToolUse, "exit 2")
	other.Matcher = "ViewImage"
	d, results := run(t, in, other)
	assert.False(t, d.Block, "the matcher selects tools by name")
	assert.Empty(t, results)
}

func TestRun_Timeout(t *testing.T) {
	h := user(hooks.PostToolUse, "sleep 10")
	h.Timeout = 200 * time.Millisecond
	started := time.Now()
	_, results := run(t, hooks.Input{Event: hooks.PostToolUse}, h)
	require.Len(t, results, 1)
	assert.Equal(t, hooks.OutcomeError, results[0].Outcome)
	assert.Contains(t, results[0].Reason, "timed out")
	assert.Less(t, time.Since(started), 3*time.Second)
}

func TestTrust(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	trust, err := hooks.LoadTrust(path)
	require.NoError(t, err)
	project := hooks.Hook{Event: hooks.Stop, Command: "exit 2", Source: hooks.SourceProject}
	r, err := hooks.New([]hooks.Hook{project}, trust, t.TempDir())
	require.NoError(t, err)

	var got []hooks.Result
	r.OnResult(func(res hooks.Result) { got = append(got, res) })
	d := r.Run(context.Background(), hooks.Input{Event: hooks.Stop})
	assert.False(t, d.Block, "an untrusted project hook does not run")
	require.Len(t, got, 1)
	assert.Equal(t, hooks.OutcomeSkipped, got[0].Outcome)

	require.NoError(t, trust.Allow("/ws", "exit 2"))
	reloaded, err := hooks.LoadTrust(path)
	require.NoError(t, err)
	assert.True(t, reloaded.Trusted("/ws", "exit 2"))
	assert.False(t, reloaded.Trusted("/ws", "exit 2 "), "a changed command needs approval again")
	assert.True(t, r.Run(context.Background(), hooks.Input{Event: hooks.Stop}).Block)
}

func TestNew_Validates(t *testing.T) {
	_, err := hooks.New([]hooks.Hook{{Event: "Nope", Command: "true"}}, nil, "")
	require.ErrorContains(t, err, "unknown hook event")
	_, err = hooks.New([]hooks.Hook{{Event: hooks.Stop}}, nil, "")
	require.ErrorContains(t, err, "no command")
	_, err = hooks.New([]hooks.Hook{{Event: hooks.PreToolUse, Command: "true", Matcher: "("}}, nil, "")
	require.ErrorContains(t, err, "matcher")
}

func TestHas_ApplyPatchAliases(t *testing.T) {
	for matcher, want := range map[string]bool{"apply_patch": true, "Edit": true, "Write": true, "Edit|Write": true, "Bash": false} {
		h := user(hooks.PreToolUse, "true")
		h.Matcher = matcher
		r, err := hooks.New([]hooks.Hook{h}, nil, t.TempDir())
		require.NoError(t, err)
		assert.Equal(t, want, r.Has(hooks.PreToolUse, "apply_patch"), matcher)
		assert.False(t, r.Has(hooks.PreToolUse, "Edit") && matcher == "apply_patch", "the alias works one way")
	}
}

// TestRun_Subagents checks SetParents: a subagent's session fires no root
// session events, its other payloads name the agent and its parent, and a
// clone keeps the lookup.
func TestRun_Subagents(t *testing.T) {
	out := filepath.Join(t.TempDir(), "payload.json")
	var hs []hooks.Hook
	for _, event := range hooks.Events {
		hs = append(hs, user(event, "cat > "+out))
	}
	r, err := hooks.New(hs, nil, t.TempDir())
	require.NoError(t, err)
	r.SetParents(func(id string) string {
		if id == "child" {
			return "root"
		}

		return ""
	})
	payload := func(r *hooks.Runner, in hooks.Input) (hooks.Input, bool) {
		t.Helper()
		require.NoError(t, os.RemoveAll(out))
		r.Run(context.Background(), in)
		data, err := os.ReadFile(out)
		if os.IsNotExist(err) {
			return hooks.Input{}, false
		}
		require.NoError(t, err)
		var got hooks.Input
		require.NoError(t, json.Unmarshal(data, &got))

		return got, true
	}

	for _, event := range hooks.RootOnly {
		_, ran := payload(r, hooks.Input{Event: event, SessionID: "child"})
		assert.False(t, ran, "%s in a subagent", event)
		_, ran = payload(r, hooks.Input{Event: event, SessionID: "root"})
		assert.True(t, ran, "%s in the root", event)
	}
	got, ran := payload(r.Clone(), hooks.Input{Event: hooks.PreToolUse, SessionID: "child", ToolName: "Bash"})
	require.True(t, ran)
	assert.Equal(t, [3]string{"child", "child", "root"}, [3]string{got.SessionID, got.AgentID, got.ParentSessionID})
	got, _ = payload(r, hooks.Input{Event: hooks.PreToolUse, SessionID: "root", ToolName: "Bash"})
	assert.Empty(t, got.AgentID+got.ParentSessionID)
	got, _ = payload(r, hooks.Input{Event: hooks.SubagentStart, SessionID: "root", AgentID: "child", AgentType: "default"})
	assert.Equal(t, [3]string{"root", "child", ""}, [3]string{got.SessionID, got.AgentID, got.ParentSessionID}, "a payload that names its agent is kept")
}

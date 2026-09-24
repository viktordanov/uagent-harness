package session_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/hooks"
	"github.com/viktordanov/uagent-harness/internal/session"
)

func withHooks(t *testing.T, hs ...hooks.Hook) *harness {
	t.Helper()
	for i := range hs {
		hs[i].Source = hooks.SourceUser
	}
	runner, err := hooks.New(hs, nil, t.TempDir())
	require.NoError(t, err)
	eng := newFakeEngine(engine.Capabilities{})
	s, err := session.Open(context.Background(), eng, session.Options{Settings: settings(), Hooks: runner})
	require.NoError(t, err)
	h := &harness{t: t, eng: eng, s: s}
	t.Cleanup(func() { _ = s.Close() })

	return h
}

func TestHooks_UserPromptSubmit(t *testing.T) {
	h := withHooks(t, hooks.Hook{
		Event:   hooks.UserPromptSubmit,
		Command: `grep -q secret && { echo 'no secrets in prompts' >&2; exit 2; } || echo 'Today is Tuesday.'`,
	})

	blocked, err := h.s.Submit("the secret is 42")
	require.NoError(t, err)
	failed := h.until(isType[session.InputFailed]).(session.InputFailed)
	assert.Equal(t, []string{blocked.ID}, failed.IDs)
	assert.Contains(t, failed.Reason, "no secrets in prompts")
	h.until(isType[session.Idle])

	_, err = h.s.Submit("what day is it?")
	require.NoError(t, err)
	run := h.nextRun()
	assert.Equal(t, []string{"what day is it?\n\nToday is Tuesday."}, texts(run.req.Messages), "plain stdout is added as context")
	ran := h.until(isType[session.HookRan]).(session.HookRan)
	assert.Equal(t, "UserPromptSubmit", ran.Event)
}

func TestHooks_KeepOrderWhileChecking(t *testing.T) {
	h := withHooks(t, hooks.Hook{Event: hooks.UserPromptSubmit, Command: "sleep 0.2"})
	_, err := h.s.Submit("first")
	require.NoError(t, err)
	_, err = h.s.Submit("second")
	require.NoError(t, err)

	run := h.nextRun()
	assert.Equal(t, []string{"first"}, texts(run.req.Messages))
	run.finish(core.StatusOK)
	run = h.nextRun()
	assert.Equal(t, []string{"second"}, texts(run.req.Messages))
}

func TestHooks_StopContinuesTheAgent(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "once")
	// Block the first stop only: the hook asks for tests, then lets the agent stop.
	h := withHooks(t, hooks.Hook{
		Event:   hooks.Stop,
		Command: `if [ -e ` + marker + ` ]; then exit 0; fi; touch ` + marker + `; echo '{"decision":"block","reason":"Now run the tests."}'`,
	})
	_, err := h.s.Submit("fix it")
	require.NoError(t, err)
	h.nextRun().finish(core.StatusOK)

	next := h.nextRun()
	assert.Equal(t, []string{"Now run the tests."}, texts(next.req.Messages))
	next.finish(core.StatusOK)
	h.until(isType[session.Idle])
	_, err = os.Stat(marker)
	require.NoError(t, err)
}

func TestHooks_StopLoopIsCapped(t *testing.T) {
	h := withHooks(t, hooks.Hook{Event: hooks.Stop, Command: `echo '{"decision":"block","reason":"again"}'`})
	_, err := h.s.Submit("go")
	require.NoError(t, err)
	for range 6 { // the first run and five continuations
		h.nextRun().finish(core.StatusOK)
	}
	notice := h.until(func(e core.Event) bool {
		n, ok := e.(session.Notice)

		return ok && n.Level == "warning"
	}).(session.Notice)
	assert.Contains(t, notice.Message, "5 times in a row")
	h.until(isType[session.Idle])
}

func TestHooks_SessionStartContextAndPostToolUse(t *testing.T) {
	out := filepath.Join(t.TempDir(), "post.json")
	h := withHooks(t,
		hooks.Hook{Event: hooks.SessionStart, Command: `echo '{"hookSpecificOutput":{"additionalContext":"Branch: main"},"systemMessage":"hello from a hook"}'`},
		hooks.Hook{Event: hooks.PostToolUse, Matcher: "Bash", Command: "cat > " + out},
	)
	notice := h.until(isType[session.Notice]).(session.Notice)
	assert.Equal(t, "hello from a hook", notice.Message)

	_, err := h.s.Submit("status?")
	require.NoError(t, err)
	run := h.nextRun()
	assert.Equal(t, []string{"status?\n\nBranch: main"}, texts(run.req.Messages))

	run.sink(core.ToolCalled{At: time.Now(), CallID: "c1", Name: "Bash", Label: "ls", Arguments: `{"command":"ls"}`})
	run.sink(core.ToolFinished{At: time.Now(), CallID: "c1", OK: true, Detail: "exit 0"})
	h.until(func(e core.Event) bool {
		r, ok := e.(session.HookRan)

		return ok && r.Event == "PostToolUse"
	})
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"tool_input":{"command":"ls"}`)
	assert.Contains(t, string(data), `"tool_response":{"success":true,"detail":"exit 0"}`)
}

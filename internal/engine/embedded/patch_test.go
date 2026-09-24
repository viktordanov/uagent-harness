package embedded_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/engine/embedded"
	"github.com/viktordanov/uagent-harness/internal/hooks"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// patchEnv is a session whose model calls apply_patch, under a sandbox
// policy of the given mode, with a directory outside the sandbox.
type patchEnv struct {
	*env
	outside string
	s       *session.Session
	ev      *events
}

type patchOpts struct {
	mode        sandbox.Mode
	interactive bool
	hooks       []hooks.Hook
}

// applyPatch is a model that applies the patch, then finishes.
func applyPatch(body string) []fakellm.Reply {
	args, _ := json.Marshal(map[string]string{"input": "*** Begin Patch\n" + body + "\n*** End Patch"}) //nolint:errchkjson // strings encode

	return []fakellm.Reply{{Calls: []fakellm.Call{{Name: "apply_patch", Args: string(args)}}}, {Text: "done"}}
}

func newPatchEnv(t *testing.T, o patchOpts, replies func(ws, outside string) []fakellm.Reply) *patchEnv {
	t.Helper()
	outside, err := os.MkdirTemp(userCache(t), "uah-patch-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(outside) })
	e := &patchEnv{env: newEnv(t), outside: outside}
	e.llm = fakellm.New(t, replies(e.Workspace, outside)...)
	policy := sandbox.Policy{Mode: o.mode, Workspace: e.Workspace}
	var runner *hooks.Runner
	if len(o.hooks) > 0 {
		runner, err = hooks.New(o.hooks, nil, e.Workspace)
		require.NoError(t, err)
	}
	eng := embedded.New(embedded.Config{
		StateDir: e.StateDir, Provider: "openai", Getenv: e.getenv, Hooks: runner,
		Sandbox: &policy, SandboxDir: filepath.Join(e.StateDir, "sandbox"),
		Approver: approval.New(approval.Config{}),
	})
	e.s, err = session.Open(context.Background(), eng, session.Options{Settings: e.settings(), Interactive: o.interactive, Hooks: runner})
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.s.Close() })
	e.ev = &events{t: t, s: e.s}
	_, err = e.s.Submit("edit the files")
	require.NoError(t, err)

	return e
}

func (e *patchEnv) lastOutput() string {
	reqs := e.llm.Requests()

	return strings.Join(reqs[len(reqs)-1].ToolOutputs, "\n")
}

func (e *patchEnv) answer(t *testing.T, a approval.Answer) session.ApprovalRequested {
	t.Helper()
	req := e.ev.until("ApprovalRequested", isA[session.ApprovalRequested]).(session.ApprovalRequested)
	require.NoError(t, e.s.Resolve(req.ID, a))

	return req
}

func TestPatch_InTheWorkspaceApplies(t *testing.T) {
	e := newPatchEnv(t, patchOpts{mode: sandbox.WorkspaceWrite}, func(ws, _ string) []fakellm.Reply {
		require.NoError(t, os.WriteFile(filepath.Join(ws, "a.txt"), []byte("one\ntwo\nthree\n"), 0o644))

		return applyPatch("*** Update File: a.txt\n@@\n one\n-two\n+TWO\n*** Add File: b.txt\n+new")
	})
	applied := e.ev.until("PatchApplied", isA[engine.PatchApplied]).(engine.PatchApplied)
	assert.Equal(t, core.StatusOK, e.ev.finished().Status)

	assert.Equal(t, "one\nTWO\nthree\n", readFile(t, filepath.Join(e.Workspace, "a.txt")))
	assert.Equal(t, "new\n", readFile(t, filepath.Join(e.Workspace, "b.txt")))
	assert.Equal(t, "Success. Updated the following files:\nA b.txt\nM a.txt\n", e.lastOutput())
	assert.Contains(t, e.llm.Requests()[0].ToolNames, "apply_patch", "offered on openai")
	require.Len(t, applied.Files, 2)
	assert.Equal(t, "a.txt", applied.Files[0].Path)
	assert.Equal(t, 1, applied.Files[0].Added)
	assert.Equal(t, 1, applied.Files[0].Removed)
	assert.Equal(t, 0, count(e.ev.all, isA[session.ApprovalRequested]), "a write in the workspace asks no one")

	runs, err := session.Load(e.StateDir, e.s.ID())
	require.NoError(t, err)
	var reloaded []engine.PatchApplied
	for _, ev := range runs[0].Events {
		if p, ok := ev.(engine.PatchApplied); ok {
			reloaded = append(reloaded, p)
		}
	}
	require.Len(t, reloaded, 1, "a reloaded transcript has the diff")
	assert.Equal(t, applied.Files, reloaded[0].Files)
}

func TestPatch_OutsideTheWorkspaceAsks(t *testing.T) {
	e := newPatchEnv(t, patchOpts{mode: sandbox.WorkspaceWrite, interactive: true}, func(_, outside string) []fakellm.Reply {
		return applyPatch("*** Add File: " + filepath.Join(outside, "x.txt") + "\n+hi")
	})
	target := filepath.Join(e.outside, "x.txt")
	req := e.answer(t, approval.Approve)
	assert.Equal(t, core.StatusOK, e.ev.finished().Status)

	assert.Equal(t, "apply_patch "+target, req.Command)
	assert.True(t, req.Escalation)
	assert.Equal(t, "the patch writes outside the writable roots", req.Justification)
	assert.Equal(t, "hi\n", readFile(t, target))
}

func TestPatch_ReadOnlyAsks(t *testing.T) {
	e := newPatchEnv(t, patchOpts{mode: sandbox.ReadOnly, interactive: true}, func(string, string) []fakellm.Reply {
		return applyPatch("*** Add File: a.txt\n+hi")
	})
	req := e.answer(t, approval.Approve)
	e.ev.finished()

	assert.Equal(t, "the sandbox is read-only", req.Justification)
	assert.Equal(t, "hi\n", readFile(t, filepath.Join(e.Workspace, "a.txt")))
}

func TestPatch_ProtectedPathAsks(t *testing.T) {
	e := newPatchEnv(t, patchOpts{mode: sandbox.WorkspaceWrite, interactive: true}, func(string, string) []fakellm.Reply {
		return applyPatch("*** Add File: .git/hooks/pre-commit\n+evil")
	})
	e.answer(t, approval.Decline)
	e.ev.finished()

	assert.NoFileExists(t, filepath.Join(e.Workspace, ".git/hooks/pre-commit"))
}

func TestPatch_DeclineReturnsAnError(t *testing.T) {
	e := newPatchEnv(t, patchOpts{mode: sandbox.WorkspaceWrite, interactive: true}, func(_, outside string) []fakellm.Reply {
		return applyPatch("*** Add File: " + filepath.Join(outside, "x.txt") + "\n+hi")
	})
	e.answer(t, approval.Decline)
	e.ev.finished()

	assert.Contains(t, e.lastOutput(), "not run: the user declined")
	assert.NoFileExists(t, filepath.Join(e.outside, "x.txt"))
	assert.Equal(t, 0, count(e.ev.all, isA[engine.PatchApplied]))
}

func TestPatch_HeadlessOutsideIsDenied(t *testing.T) {
	e := newPatchEnv(t, patchOpts{mode: sandbox.WorkspaceWrite}, func(_, outside string) []fakellm.Reply {
		return applyPatch("*** Add File: " + filepath.Join(outside, "x.txt") + "\n+hi")
	})
	e.ev.finished()

	assert.Contains(t, e.lastOutput(), "no user can approve it")
	assert.NoFileExists(t, filepath.Join(e.outside, "x.txt"))
}

func TestPatch_FullAccessAppliesEverything(t *testing.T) {
	e := newPatchEnv(t, patchOpts{mode: sandbox.FullAccess}, func(_, outside string) []fakellm.Reply {
		return applyPatch("*** Add File: " + filepath.Join(outside, "x.txt") + "\n+hi")
	})
	e.ev.finished()

	assert.Equal(t, "hi\n", readFile(t, filepath.Join(e.outside, "x.txt")))
}

func TestPatch_VerificationFails(t *testing.T) {
	e := newPatchEnv(t, patchOpts{mode: sandbox.WorkspaceWrite}, func(string, string) []fakellm.Reply {
		return applyPatch("*** Update File: missing.txt\n@@\n-a\n+b")
	})
	e.ev.finished()

	assert.Contains(t, e.lastOutput(), "apply_patch verification failed: Failed to read file to update")
}

func TestPatch_Hooks(t *testing.T) {
	log := filepath.Join(t.TempDir(), "hook.log")
	e := newPatchEnv(t, patchOpts{
		mode: sandbox.WorkspaceWrite, interactive: true,
		hooks: []hooks.Hook{
			// PreToolUse sees Codex's {"command": patch}; the Edit alias matches.
			{Event: hooks.PreToolUse, Matcher: "Edit", Source: hooks.SourceUser, Command: "cat >> " + log},
			{Event: hooks.PostToolUse, Matcher: "apply_patch", Source: hooks.SourceUser, Command: "cat >> " + log},
			// PermissionRequest answers for the user.
			{Event: hooks.PermissionRequest, Matcher: "Write", Source: hooks.SourceUser, Command: `echo '{"hookSpecificOutput":{"permissionDecision":"allow"}}'`},
		},
	}, func(_, outside string) []fakellm.Reply {
		return applyPatch("*** Add File: " + filepath.Join(outside, "x.txt") + "\n+hi")
	})
	e.ev.finished()
	e.ev.idle()

	assert.Equal(t, "hi\n", readFile(t, filepath.Join(e.outside, "x.txt")), "the PermissionRequest hook approved it")
	assert.Eventually(t, func() bool { return strings.Count(readFile(t, log), `"tool_name":"apply_patch"`) == 2 }, waitTimeout, 50*1e6)
	got := readFile(t, log)
	assert.Contains(t, got, `"hook_event_name":"PreToolUse"`)
	assert.Contains(t, got, `"hook_event_name":"PostToolUse"`)
	assert.Contains(t, got, `"command":"*** Begin Patch\n*** Add File: `)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(data)
}

func count(all []core.Event, what func(core.Event) bool) int {
	n := 0
	for _, e := range all {
		if what(e) {
			n++
		}
	}

	return n
}

// TestPatch_FollowsTheMode applies patches under the run's live permission
// mode: auto lets the reviewer approve a write outside the workspace
// without asking, and a switch to read only makes the next workspace
// write ask.
func TestPatch_FollowsTheMode(t *testing.T) {
	allow := fakellm.Reply{Text: `{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"The user asked for it."}`}
	var e *approvalEnv
	e = newApprovalEnv(t, approvalOpts{interactive: true, mode: approval.ModeAuto}, func(outside string) []fakellm.Reply {
		first := applyPatch("*** Add File: " + filepath.Join(outside, "x.txt") + "\n+hi")[0]
		calls := applyPatch("*** Add File: in.txt\n+hi")[0].Calls
		second := fakellm.Reply{From: func(fakellm.Request) fakellm.Reply {
			_, err := e.s.SetSettings(e.settings().WithMode(approval.ModeReadOnly))
			require.NoError(t, err)

			return fakellm.Reply{Calls: calls}
		}}

		return []fakellm.Reply{first, allow, second, {Text: "done"}}
	})
	e.run(t)
	req := e.approve(t, approval.Decline)

	assert.Equal(t, core.StatusOK, e.ev.finished().Status)
	assert.Equal(t, "hi\n", readFile(t, filepath.Join(e.outside, "x.txt")), "the reviewer approved it in auto mode")
	assert.Equal(t, 1, countKind[engine.AutoReviewed](e.ev.all))
	assert.Equal(t, "the sandbox is read-only", req.Justification, "read only asks after the switch")
	assert.NoFileExists(t, filepath.Join(e.Workspace, "in.txt"))
}

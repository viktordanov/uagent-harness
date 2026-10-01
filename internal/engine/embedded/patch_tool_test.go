package embedded_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/hooks"
	"github.com/viktordanov/uah/internal/patch"
	"github.com/viktordanov/uah/internal/sandbox"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/testing/fakellm"
)

// wireItem is an input item of a request, as the provider reads it.
type wireItem struct {
	Type      string          `json:"type"`
	CallID    string          `json:"call_id"`
	Name      string          `json:"name"`
	Input     string          `json:"input"`
	Arguments string          `json:"arguments"`
	Output    json.RawMessage `json:"output"`
}

func wireItems(t *testing.T, req fakellm.Request, types ...string) []wireItem {
	t.Helper()
	var out []wireItem
	for _, raw := range req.Input {
		var it wireItem
		require.NoError(t, json.Unmarshal(raw, &it))
		for _, typ := range types {
			if it.Type == typ {
				out = append(out, it)
			}
		}
	}

	return out
}

// patchTool is the apply_patch definition a request offered.
func patchTool(t *testing.T, req fakellm.Request) map[string]any {
	t.Helper()
	for _, raw := range req.ToolDefs {
		var def map[string]any
		require.NoError(t, json.Unmarshal(raw, &def))
		if def["name"] == patch.ToolName {
			return def
		}
	}
	t.Fatal("apply_patch is not offered")

	return nil
}

// TestPatch_IsCodexsCustomTool: apply_patch is Codex's custom tool with its
// grammar; the model's raw patch applies, shows as an edit of its files with
// its diff, and goes back to the model as a custom tool call and its output.
func TestPatch_IsCodexsCustomTool(t *testing.T) {
	raw := "*** Begin Patch\n*** Add File: b.txt\n+say \"new\"\n*** End Patch\n"
	e := newPatchEnv(t, patchOpts{mode: sandbox.WorkspaceWrite}, func(string, string) []fakellm.Reply {
		return applyPatch("*** Add File: b.txt\n+say \"new\"")
	})
	applied := e.ev.until("PatchApplied", isA[engine.PatchApplied]).(engine.PatchApplied)
	assert.Equal(t, core.StatusOK, e.ev.finished().Status)

	assert.Equal(t, "say \"new\"\n", readFile(t, filepath.Join(e.Workspace, "b.txt")))
	require.Len(t, applied.Files, 1)
	assert.Equal(t, "b.txt", applied.Files[0].Path)

	reqs := e.llm.Requests()
	require.Len(t, reqs, 2)
	def := patchTool(t, reqs[0])
	assert.Equal(t, "custom", def["type"])
	assert.Equal(t, patch.Description, def["description"])
	assert.Equal(t, map[string]any{"type": "grammar", "syntax": "lark", "definition": patch.Grammar}, def["format"])
	assert.NotContains(t, def, "parameters")

	calls := wireItems(t, reqs[1], "custom_tool_call", "function_call")
	require.Len(t, calls, 1)
	assert.Equal(t, "custom_tool_call", calls[0].Type)
	assert.Equal(t, raw, calls[0].Input)
	outputs := wireItems(t, reqs[1], "custom_tool_call_output", "function_call_output")
	require.Len(t, outputs, 1)
	assert.Equal(t, "custom_tool_call_output", outputs[0].Type)
	assert.Equal(t, calls[0].CallID, outputs[0].CallID)
	assert.Equal(t, "Success. Updated the following files:\nA b.txt\n", e.lastOutput())

	var called core.ToolCalled
	for _, ev := range e.ev.all {
		if c, ok := ev.(core.ToolCalled); ok {
			called = c
		}
	}
	assert.Equal(t, raw, called.Arguments)
	assert.Equal(t, "b.txt", patch.Describe(called.Arguments), "the transcript's EDIT line")

	runs, err := session.Load(e.StateDir, e.s.ID())
	require.NoError(t, err)
	assert.Equal(t, 1, count(runs[0].Events, isA[engine.PatchApplied]), "a reloaded transcript has the diff")
}

// TestPatch_InvalidPatch: an input that is not a patch gets the parser's
// message.
func TestPatch_InvalidPatch(t *testing.T) {
	e := newPatchEnv(t, patchOpts{mode: sandbox.WorkspaceWrite}, func(string, string) []fakellm.Reply {
		return []fakellm.Reply{{Calls: []fakellm.Call{{Name: "apply_patch", Args: "*** Add File: a.txt\n+x", Custom: true}}}, {Text: "done"}}
	})
	e.ev.finished()

	assert.Contains(t, e.lastOutput(), "apply_patch verification failed: invalid patch")
	assert.NotContains(t, e.lastOutput(), "JSON")
}

// TestPatch_HookUpdatesThePatch: PreToolUse and PostToolUse hooks see
// Codex's {"command": patch}, and a hook's updatedInput replaces it.
func TestPatch_HookUpdatesThePatch(t *testing.T) {
	log := filepath.Join(t.TempDir(), "hook.log")
	update := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","updatedInput":{"command":"*** Begin Patch\n*** Add File: hooked.txt\n+from the hook\n*** End Patch"}}}`
	e := newPatchEnv(t, patchOpts{
		mode: sandbox.WorkspaceWrite, interactive: true,
		hooks: []hooks.Hook{
			{Event: hooks.PreToolUse, Matcher: "Edit", Source: hooks.SourceUser, Command: "cat >> " + log + "; printf '%s' '" + update + "'"},
			{Event: hooks.PostToolUse, Matcher: "apply_patch", Source: hooks.SourceUser, Command: "cat >> " + log},
		},
	}, func(_, outside string) []fakellm.Reply {
		return applyPatch("*** Add File: " + filepath.Join(outside, "x.txt") + "\n+hi")
	})
	e.ev.finished()
	e.ev.idle()

	assert.Eventually(t, func() bool { return strings.Contains(readFile(t, log), `"hook_event_name":"PostToolUse"`) }, waitTimeout, 50*1e6)
	got := readFile(t, log)
	assert.Equal(t, 2, strings.Count(got, `"command":"*** Begin Patch\n*** Add File: `+filepath.Join(e.outside, "x.txt")), "PreToolUse and PostToolUse")
	assert.Contains(t, got, `"command":"*** Begin Patch\n*** Add File: `+filepath.Join(e.outside, "x.txt"))
	assert.Contains(t, got, `"file_path":"`+filepath.Join(e.outside, "x.txt")+`"`)
	assert.Equal(t, "from the hook\n", readFile(t, filepath.Join(e.Workspace, "hooked.txt")), "the hook's patch applied")
	assert.NoFileExists(t, filepath.Join(e.outside, "x.txt"))
	assert.True(t, strings.HasPrefix(e.lastOutput(), "Success."))
}

// TestPatch_ApproverSeesThePatch: the approver of a patch outside the
// workspace (a PermissionRequest hook here, the auto-reviewer in auto mode)
// sees Codex's {"command": patch}.
func TestPatch_ApproverSeesThePatch(t *testing.T) {
	log := filepath.Join(t.TempDir(), "permission.log")
	e := newPatchEnv(t, patchOpts{
		mode: sandbox.WorkspaceWrite, interactive: true,
		hooks: []hooks.Hook{{
			Event: hooks.PermissionRequest, Matcher: "apply_patch", Source: hooks.SourceUser,
			Command: "cat >> " + log + `; echo '{"hookSpecificOutput":{"permissionDecision":"allow"}}'`,
		}},
	}, func(_, outside string) []fakellm.Reply {
		return applyPatch("*** Add File: " + filepath.Join(outside, "x.txt") + "\n+hi")
	})
	e.ev.finished()

	target := filepath.Join(e.outside, "x.txt")
	assert.Equal(t, "hi\n", readFile(t, target))
	got := readFile(t, log)
	assert.Contains(t, got, `"command":"*** Begin Patch\n*** Add File: `+target+`\n+hi\n*** End Patch\n"`)
	assert.Contains(t, got, `"file_paths":["`+target+`"]`)
}

// TestPatch_ResumesFunctionCalls: a session recorded when apply_patch was
// a function tool resumes, rewinds, and goes on with the custom tool: its
// old calls go back as they were recorded, a function call with the patch
// in "input" and its output, which the Responses API takes for a tool the
// request no longer declares, as Codex sends its history.
func TestPatch_ResumesFunctionCalls(t *testing.T) {
	old := "*** Begin Patch\n*** Add File: a.txt\n+old\n*** End Patch"
	args, err := json.Marshal(map[string]string{"input": old})
	require.NoError(t, err)
	replies := append([]fakellm.Reply{{Calls: []fakellm.Call{{Name: "apply_patch", Args: string(args)}}}, {Text: "done"}},
		fakellm.Reply{Text: "two"}, fakellm.Reply{Text: "two, again"})
	replies = append(replies, applyPatch("*** Update File: a.txt\n@@\n-old\n+new")...)
	e := newEnv(t, replies...)
	s, ev := e.open(t, e.embedded(), "")
	send(t, s, ev, "edit")
	second := send(t, s, ev, "second")
	require.NoError(t, s.Rewind(second))
	send(t, s, ev, "second, edited")
	id := s.ID()
	require.NoError(t, s.Close())

	s2, ev2 := e.open(t, e.embedded(), id)
	send(t, s2, ev2, "third")

	assert.Equal(t, "new\n", readFile(t, filepath.Join(e.Workspace, "a.txt")))
	reqs := e.llm.Requests()
	require.Len(t, reqs, 6)
	for _, req := range reqs[2:] {
		assert.Equal(t, "custom", patchTool(t, req)["type"])
		calls := wireItems(t, req, "function_call")
		require.Len(t, calls, 1)
		assert.Equal(t, patch.ToolName, calls[0].Name)
		assert.Equal(t, string(args), calls[0].Arguments)
		outputs := wireItems(t, req, "function_call_output")
		require.Len(t, outputs, 1)
		assert.Equal(t, calls[0].CallID, outputs[0].CallID)
	}
	assert.Equal(t, []string{"edit", "second, edited", "third"}, reqs[4].UserTexts)
	custom := wireItems(t, reqs[5], "custom_tool_call", "custom_tool_call_output")
	require.Len(t, custom, 2, "the new call is a custom one")
	assert.Equal(t, "Success. Updated the following files:\nM a.txt\n", strings.Join(reqs[5].ToolOutputs[len(reqs[5].ToolOutputs)-1:], ""))

	runs, err := session.Load(e.StateDir, id)
	require.NoError(t, err)
	var describes []string
	for _, r := range runs {
		for _, ev := range r.Events {
			if c, ok := ev.(core.ToolCalled); ok {
				describes = append(describes, patch.Describe(c.Arguments))
			}
		}
	}
	assert.Equal(t, []string{"a.txt", "a.txt"}, describes, "the transcript reads both forms")
}

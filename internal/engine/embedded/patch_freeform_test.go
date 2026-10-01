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
	"github.com/viktordanov/uah/internal/engine/embedded"
	"github.com/viktordanov/uah/internal/hooks"
	"github.com/viktordanov/uah/internal/patch"
	"github.com/viktordanov/uah/internal/sandbox"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/testing/fakellm"
)

// applyFreeformPatch is a model that applies the patch through the
// freeform tool, its input the raw patch, then finishes.
func applyFreeformPatch(body string) []fakellm.Reply {
	input := "*** Begin Patch\n" + body + "\n*** End Patch\n"

	return []fakellm.Reply{{Calls: []fakellm.Call{{Name: "apply_patch", Args: input, Custom: true}}}, {Text: "done"}}
}

// wireItem is an input item of a request, as the provider reads it.
type wireItem struct {
	Type   string          `json:"type"`
	CallID string          `json:"call_id"`
	Input  string          `json:"input"`
	Output json.RawMessage `json:"output"`
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

// TestFreeformPatch_Applies: with the experiment, apply_patch is Codex's
// custom tool with its grammar; the model's raw patch applies, shows as an
// edit of its files with its diff, and goes back to the model as a custom
// tool call and its output.
func TestFreeformPatch_Applies(t *testing.T) {
	raw := "*** Begin Patch\n*** Add File: b.txt\n+say \"new\"\n*** End Patch\n"
	e := newPatchEnv(t, patchOpts{mode: sandbox.WorkspaceWrite, freeform: true}, func(string, string) []fakellm.Reply {
		return applyFreeformPatch("*** Add File: b.txt\n+say \"new\"")
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
	assert.Equal(t, patch.FreeformDescription, def["description"])
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

// TestFreeformPatch_OffByDefault: without the experiment apply_patch stays
// a function tool with the patch in "input".
func TestFreeformPatch_OffByDefault(t *testing.T) {
	e := newPatchEnv(t, patchOpts{mode: sandbox.WorkspaceWrite}, func(string, string) []fakellm.Reply {
		return applyPatch("*** Add File: b.txt\n+new")
	})
	e.ev.finished()

	reqs := e.llm.Requests()
	def := patchTool(t, reqs[0])
	assert.Equal(t, "function", def["type"])
	assert.NotContains(t, def, "format")
	assert.Len(t, wireItems(t, reqs[1], "function_call"), 1)
	assert.Empty(t, wireItems(t, reqs[1], "custom_tool_call", "custom_tool_call_output"))
	assert.Equal(t, "new\n", readFile(t, filepath.Join(e.Workspace, "b.txt")))
}

// TestFreeformPatch_InvalidPatch: a freeform input that is not a patch gets
// the parser's message, not the function form's.
func TestFreeformPatch_InvalidPatch(t *testing.T) {
	e := newPatchEnv(t, patchOpts{mode: sandbox.WorkspaceWrite, freeform: true}, func(string, string) []fakellm.Reply {
		return []fakellm.Reply{{Calls: []fakellm.Call{{Name: "apply_patch", Args: "*** Add File: a.txt\n+x", Custom: true}}}, {Text: "done"}}
	})
	e.ev.finished()

	assert.Contains(t, e.lastOutput(), "apply_patch verification failed: invalid patch")
	assert.NotContains(t, e.lastOutput(), "JSON")
}

// TestFreeformPatch_Hooks: PreToolUse and PostToolUse hooks see Codex's
// {"command": patch} for a raw patch, and a hook's updatedInput replaces it.
func TestFreeformPatch_Hooks(t *testing.T) {
	log := filepath.Join(t.TempDir(), "hook.log")
	update := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","updatedInput":{"command":"*** Begin Patch\n*** Add File: hooked.txt\n+from the hook\n*** End Patch"}}}`
	e := newPatchEnv(t, patchOpts{
		mode: sandbox.WorkspaceWrite, interactive: true, freeform: true,
		hooks: []hooks.Hook{
			{Event: hooks.PreToolUse, Matcher: "Edit", Source: hooks.SourceUser, Command: "cat >> " + log + "; printf '%s' '" + update + "'"},
			{Event: hooks.PostToolUse, Matcher: "apply_patch", Source: hooks.SourceUser, Command: "cat >> " + log},
		},
	}, func(_, outside string) []fakellm.Reply {
		return applyFreeformPatch("*** Add File: " + filepath.Join(outside, "x.txt") + "\n+hi")
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

// TestFreeformPatch_OutsideAsks: a raw patch outside the workspace asks
// as the function form does; the approver (a PermissionRequest hook here,
// the auto-reviewer in auto mode) sees Codex's {"command": patch}.
func TestFreeformPatch_OutsideAsks(t *testing.T) {
	log := filepath.Join(t.TempDir(), "permission.log")
	e := newPatchEnv(t, patchOpts{
		mode: sandbox.WorkspaceWrite, interactive: true, freeform: true,
		hooks: []hooks.Hook{{
			Event: hooks.PermissionRequest, Matcher: "apply_patch", Source: hooks.SourceUser,
			Command: "cat >> " + log + `; echo '{"hookSpecificOutput":{"permissionDecision":"allow"}}'`,
		}},
	}, func(_, outside string) []fakellm.Reply {
		return applyFreeformPatch("*** Add File: " + filepath.Join(outside, "x.txt") + "\n+hi")
	})
	e.ev.finished()

	target := filepath.Join(e.outside, "x.txt")
	assert.Equal(t, "hi\n", readFile(t, target))
	got := readFile(t, log)
	assert.Contains(t, got, `"command":"*** Begin Patch\n*** Add File: `+target+`\n+hi\n*** End Patch\n"`)
	assert.Contains(t, got, `"file_paths":["`+target+`"]`)
}

// TestFreeformPatch_SurvivesRewindAndResume: the session file keeps a
// freeform call as one, so a rewound and a resumed session send it back as
// a custom tool call with its output, even from an engine without the
// experiment.
func TestFreeformPatch_SurvivesRewindAndResume(t *testing.T) {
	replies := append(applyFreeformPatch("*** Add File: b.txt\n+new"),
		fakellm.Reply{Text: "two"}, fakellm.Reply{Text: "two, again"}, fakellm.Reply{Text: "three"})
	e := newEnv(t, replies...)
	freeform := embedded.New(embedded.Config{StateDir: e.StateDir, Provider: "openai", Getenv: func(key string) string {
		if key == "UAH_EXPERIMENTS" {
			return "freeform-patch"
		}

		return e.getenv(key)
	}})
	s, ev := e.open(t, freeform, "")
	send(t, s, ev, "edit")
	second := send(t, s, ev, "second")
	require.NoError(t, s.Rewind(second))
	send(t, s, ev, "second, edited")
	id := s.ID()
	require.NoError(t, s.Close())

	s2, ev2 := e.open(t, e.embedded(), id)
	send(t, s2, ev2, "third")

	reqs := e.llm.Requests()
	require.Len(t, reqs, 5)
	for _, req := range reqs[2:] {
		calls := wireItems(t, req, "custom_tool_call", "function_call")
		require.Len(t, calls, 1)
		assert.Equal(t, "custom_tool_call", calls[0].Type)
		assert.Equal(t, "*** Begin Patch\n*** Add File: b.txt\n+new\n*** End Patch\n", calls[0].Input)
		outputs := wireItems(t, req, "custom_tool_call_output", "function_call_output")
		require.Len(t, outputs, 1)
		assert.Equal(t, "custom_tool_call_output", outputs[0].Type)
	}
	assert.Equal(t, []string{"edit", "second, edited", "third"}, reqs[4].UserTexts)
	assert.Equal(t, "function", patchTool(t, reqs[4])["type"], "the resumed engine offers the function tool")
}

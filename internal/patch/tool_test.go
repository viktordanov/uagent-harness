package patch_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/patch"
)

const rawPatch = "*** Begin Patch\n*** Add File: a.txt\n+say \"hi\"\n*** Update File: b.go\n@@\n-x\n+y\n*** End Patch\n"

// TestTool_ReadersTakeRawPatches: a call's input is the patch itself,
// and every reader of the tool's arguments takes it.
func TestTool_ReadersTakeRawPatches(t *testing.T) {
	text, err := patch.ParseArgs(rawPatch)
	require.NoError(t, err)
	assert.Equal(t, rawPatch, text)
	text, err = patch.ParseArgs("\n  " + rawPatch)
	require.NoError(t, err)
	assert.Equal(t, "\n  "+rawPatch, text, "the patch's own leniency trims it")
	assert.Equal(t, "a.txt, b.go", patch.Describe(rawPatch))
	assert.JSONEq(t, `{"command":`+quote(rawPatch)+`,"file_path":"a.txt","file_paths":["a.txt","b.go"]}`, string(patch.HookInput(rawPatch)))

	for _, args := range []string{"", "*** Add File: a.txt\n+x", `{"input":""}`, "{"} {
		_, err := patch.ParseArgs(args)
		require.ErrorIs(t, err, patch.ErrNoInput, args)
	}
}

// TestTool_ReadersTakeRecordedFunctionCalls: a call recorded when
// apply_patch was a function tool has the patch in "input", and every
// reader takes it as it takes a raw patch.
func TestTool_ReadersTakeRecordedFunctionCalls(t *testing.T) {
	args := `{"input":` + quote(rawPatch) + `}`
	text, err := patch.ParseArgs(args)
	require.NoError(t, err)
	assert.Equal(t, rawPatch, text)
	assert.Equal(t, "a.txt, b.go", patch.Describe(args))
	assert.Equal(t, string(patch.HookInput(rawPatch)), string(patch.HookInput(args)))
}

// TestTool_HookUpdatedInputIsThePatch: a hook's updatedInput replaces the
// patch through its "command", or "input".
func TestTool_HookUpdatedInputIsThePatch(t *testing.T) {
	for _, in := range []string{`{"command":` + quote(rawPatch) + `}`, `{"input":` + quote(rawPatch) + `}`} {
		back, err := patch.FromHookInput([]byte(in))
		require.NoError(t, err)
		assert.Equal(t, rawPatch, back, in)
	}
	_, err := patch.FromHookInput([]byte(`{"file_path":"a.txt"}`))
	require.ErrorIs(t, err, patch.ErrNoInput)
}

// TestTool_GrammarIsCodexs: the grammar is Codex's file, whose start
// rule the provider samples a patch from.
func TestTool_GrammarIsCodexs(t *testing.T) {
	data, err := os.ReadFile("apply_patch.lark")
	require.NoError(t, err)
	assert.Equal(t, string(data), patch.Grammar)
	assert.True(t, strings.HasPrefix(patch.Grammar, "start: begin_patch hunk+ end_patch\n"))
	assert.Contains(t, patch.Description, "FREEFORM")
}

func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

	return `"` + r.Replace(s) + `"`
}

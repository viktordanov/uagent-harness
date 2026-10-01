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

// TestFreeform_ReadersTakeRawPatches: a freeform call's input is the patch
// itself, and every reader of the tool's arguments takes it as it takes
// the function form's {"input": patch}.
func TestFreeform_ReadersTakeRawPatches(t *testing.T) {
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
		assert.False(t, patch.IsFreeform(args), args)
	}
	assert.True(t, patch.IsFreeform(rawPatch))
	assert.False(t, patch.IsFreeform(`{"input":`+quote(rawPatch)+`}`))
}

// TestFreeform_GrammarIsCodexs: the grammar is Codex's file, whose start
// rule the provider samples a patch from.
func TestFreeform_GrammarIsCodexs(t *testing.T) {
	data, err := os.ReadFile("apply_patch.lark")
	require.NoError(t, err)
	assert.Equal(t, string(data), patch.Grammar)
	assert.True(t, strings.HasPrefix(patch.Grammar, "start: begin_patch hunk+ end_patch\n"))
	assert.Contains(t, patch.FreeformDescription, "FREEFORM")
}

func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

	return `"` + r.Replace(s) + `"`
}

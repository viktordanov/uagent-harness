package patch_test

// The cases are ported from openai/codex rust-v0.156.1 (Apache License
// 2.0): codex-rs/apply-patch/src/parser.rs and streaming_parser.rs tests.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/patch"
)

func TestParse_Errors(t *testing.T) {
	for _, tc := range []struct{ name, patch, want string }{
		{"no begin", "bad", "invalid patch: The first line of the patch must be '*** Begin Patch'"},
		{"no end", "*** Begin Patch\nbad", "invalid patch: The last line of the patch must be '*** End Patch'"},
		{"empty update", "*** Begin Patch\n*** Update File: test.py\n*** End Patch", "invalid hunk at line 2, Update file hunk for path 'test.py' is empty"},
		{
			"bad header", "*** Begin Patch\n*** Frobnicate File: foo\n*** End Patch",
			"invalid hunk at line 2, '*** Frobnicate File: foo' is not a valid hunk header. Valid hunk headers: '*** Add File: {path}', '*** Delete File: {path}', '*** Update File: {path}'",
		},
		{
			"bad update line", "*** Begin Patch\n*** Update File: a.py\n@@\n+x\nbad\n*** End Patch",
			"invalid hunk at line 5, Expected update hunk to start with a @@ context marker, got: 'bad'",
		},
		{
			"empty chunk", "*** Begin Patch\n*** Update File: a.py\n@@\n*** End Patch",
			"invalid hunk at line 4, Update hunk does not contain any lines",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := patch.Parse(tc.patch)
			require.Error(t, err)
			assert.Equal(t, tc.want, err.Error())
		})
	}
}

func TestParse_AllOps(t *testing.T) {
	hunks, err := patch.Parse("*** Begin Patch\n*** Add File: path/add.py\n+abc\n+def\n*** Delete File: path/delete.py\n" +
		"*** Update File: path/update.py\n*** Move to: path/update2.py\n@@ def f():\n-    pass\n+    return 123\n*** End Patch")
	require.NoError(t, err)
	assert.Equal(t, []patch.Hunk{
		{Op: patch.Add, Path: "path/add.py", Contents: "abc\ndef\n"},
		{Op: patch.Delete, Path: "path/delete.py"},
		{Op: patch.Update, Path: "path/update.py", MovePath: "path/update2.py", Chunks: []patch.Chunk{
			{Context: "def f():", HasContext: true, Old: []string{"    pass"}, New: []string{"    return 123"}},
		}},
	}, hunks)
}

func TestParse_Lenient(t *testing.T) {
	t.Run("whitespace around markers", func(t *testing.T) {
		hunks, err := patch.Parse("*** Begin Patch \n*** Add File: foo\n+hi\n *** End Patch")
		require.NoError(t, err)
		assert.Equal(t, []patch.Hunk{{Op: patch.Add, Path: "foo", Contents: "hi\n"}}, hunks)
	})
	t.Run("heredoc", func(t *testing.T) {
		hunks, err := patch.Parse("<<'EOF'\n*** Begin Patch\n*** Add File: foo\n+hi\n*** End Patch\nEOF\n")
		require.NoError(t, err)
		assert.Len(t, hunks, 1)
	})
	t.Run("first chunk without @@", func(t *testing.T) {
		hunks, err := patch.Parse("*** Begin Patch\n*** Update File: file2.py\n import foo\n+bar\n*** End Patch")
		require.NoError(t, err)
		assert.Equal(t, []patch.Chunk{{Old: []string{"import foo"}, New: []string{"import foo", "bar"}}}, hunks[0].Chunks)
	})
	t.Run("update then add", func(t *testing.T) {
		hunks, err := patch.Parse("*** Begin Patch\n*** Update File: file.py\n@@\n+line\n*** Add File: other.py\n+content\n*** End Patch")
		require.NoError(t, err)
		assert.Equal(t, []patch.Hunk{
			{Op: patch.Update, Path: "file.py", Chunks: []patch.Chunk{{New: []string{"line"}}}},
			{Op: patch.Add, Path: "other.py", Contents: "content\n"},
		}, hunks)
	})
	t.Run("end of file marker", func(t *testing.T) {
		hunks, err := patch.Parse("*** Begin Patch\n*** Update File: file.txt\n@@\n+quux\n*** End of File\n\n*** End Patch")
		require.NoError(t, err)
		assert.Equal(t, []patch.Chunk{{New: []string{"quux"}, EndOfFile: true}}, hunks[0].Chunks)
	})
	t.Run("indented markers stay context", func(t *testing.T) {
		hunks, err := patch.Parse("*** Begin Patch\n*** Update File: a.txt\n@@\n  *** Add File: b.txt\n+x\n*** End Patch")
		require.NoError(t, err)
		require.Len(t, hunks, 1)
		assert.Equal(t, []string{" *** Add File: b.txt"}, hunks[0].Chunks[0].Old)
	})
	t.Run("bare empty lines are context", func(t *testing.T) {
		hunks, err := patch.Parse("*** Begin Patch\n*** Update File: a.txt\n@@\n a\n\n-b\n*** End Patch")
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "", "b"}, hunks[0].Chunks[0].Old)
	})
	t.Run("CRLF", func(t *testing.T) {
		hunks, err := patch.Parse("*** Begin Patch\r\n*** Add File: foo\r\n+hi\r\n*** End Patch\r\n")
		require.NoError(t, err)
		assert.Equal(t, "hi\n", hunks[0].Contents)
	})
}

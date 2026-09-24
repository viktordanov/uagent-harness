package patch_test

// The cases are ported from openai/codex rust-v0.156.1 (Apache License
// 2.0): the tests in codex-rs/apply-patch/src/lib.rs and
// seek_sequence.rs.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/patch"
)

// apply parses, computes, and writes a patch in dir, as the tool does, and
// returns the summary.
func apply(t *testing.T, dir, body string) (string, error) {
	t.Helper()
	hunks, err := patch.Parse("*** Begin Patch\n" + body + "\n*** End Patch")
	if err != nil {
		return "", err
	}
	changes, err := patch.Compute(dir, hunks)
	if err != nil {
		return "", err
	}
	if err := patch.Write(changes); err != nil {
		return "", err
	}

	return patch.Summary(changes), nil
}

func write(t *testing.T, path, text string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(text), 0o644))
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(data)
}

func TestApply_AddFile(t *testing.T) {
	dir := t.TempDir()
	out, err := apply(t, dir, "*** Add File: sub/add.txt\n+ab\n+cd")
	require.NoError(t, err)
	assert.Equal(t, "Success. Updated the following files:\nA sub/add.txt\n", out)
	assert.Equal(t, "ab\ncd\n", read(t, filepath.Join(dir, "sub/add.txt")))
}

func TestApply_DeleteFile(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "del.txt"), "x")
	out, err := apply(t, dir, "*** Delete File: del.txt")
	require.NoError(t, err)
	assert.Equal(t, "Success. Updated the following files:\nD del.txt\n", out)
	assert.NoFileExists(t, filepath.Join(dir, "del.txt"))
}

func TestApply_UpdateWithContext(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "update.txt"), "foo\nbar\n")
	out, err := apply(t, dir, "*** Update File: update.txt\n@@\n foo\n-bar\n+baz")
	require.NoError(t, err)
	assert.Equal(t, "Success. Updated the following files:\nM update.txt\n", out)
	assert.Equal(t, "foo\nbaz\n", read(t, filepath.Join(dir, "update.txt")))
}

func TestApply_MoveFile(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "src.txt"), "line\n")
	out, err := apply(t, dir, "*** Update File: src.txt\n*** Move to: dst.txt\n@@\n-line\n+line2")
	require.NoError(t, err)
	assert.Equal(t, "Success. Updated the following files:\nM dst.txt\n", out)
	assert.NoFileExists(t, filepath.Join(dir, "src.txt"))
	assert.Equal(t, "line2\n", read(t, filepath.Join(dir, "dst.txt")))
}

func TestApply_MultipleChunks(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "multi.txt"), "foo\nbar\nbaz\nqux\n")
	_, err := apply(t, dir, "*** Update File: multi.txt\n@@\n foo\n-bar\n+BAR\n@@\n baz\n-qux\n+QUX")
	require.NoError(t, err)
	assert.Equal(t, "foo\nBAR\nbaz\nQUX\n", read(t, filepath.Join(dir, "multi.txt")))
}

func TestApply_InterleavedChangesAndEndOfFile(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "interleaved.txt"), "a\nb\nc\nd\ne\nf\n")
	_, err := apply(t, dir, "*** Update File: interleaved.txt\n@@\n a\n-b\n+B\n@@\n c\n d\n-e\n+E\n@@\n f\n+g\n*** End of File")
	require.NoError(t, err)
	assert.Equal(t, "a\nB\nc\nd\nE\nf\ng\n", read(t, filepath.Join(dir, "interleaved.txt")))
}

func TestApply_PureAdditionThenRemoval(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "panic.txt"), "line1\nline2\nline3\n")
	_, err := apply(t, dir, "*** Update File: panic.txt\n@@\n+after-context\n+second-line\n@@\n line1\n-line2\n-line3\n+line2-replacement")
	require.NoError(t, err)
	assert.Equal(t, "line1\nline2-replacement\nafter-context\nsecond-line\n", read(t, filepath.Join(dir, "panic.txt")))
}

func TestApply_FuzzyContext(t *testing.T) {
	t.Run("unicode punctuation", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "unicode.py"), "import asyncio  # local import – avoids top‑level dep\n")
		_, err := apply(t, dir, "*** Update File: unicode.py\n@@\n-import asyncio  # local import - avoids top-level dep\n+import asyncio  # HELLO")
		require.NoError(t, err)
		assert.Equal(t, "import asyncio  # HELLO\n", read(t, filepath.Join(dir, "unicode.py")))
	})
	t.Run("trailing whitespace", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "ws.txt"), "foo   \nbar\n")
		_, err := apply(t, dir, "*** Update File: ws.txt\n@@\n foo\n-bar\n+baz")
		require.NoError(t, err)
		// As Codex in its default mode, the matched lines take the patch's text.
		assert.Equal(t, "foo\nbaz\n", read(t, filepath.Join(dir, "ws.txt")))
	})
	t.Run("surrounding whitespace", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "ws.txt"), "    foo\nbar\n")
		_, err := apply(t, dir, "*** Update File: ws.txt\n@@\n foo\n-bar\n+baz")
		require.NoError(t, err)
		assert.Equal(t, "foo\nbaz\n", read(t, filepath.Join(dir, "ws.txt")))
	})
	t.Run("@@ context narrows the place", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "f.py"), "def a():\n    pass\ndef b():\n    pass\n")
		_, err := apply(t, dir, "*** Update File: f.py\n@@ def b():\n-    pass\n+    return 1")
		require.NoError(t, err)
		assert.Equal(t, "def a():\n    pass\ndef b():\n    return 1\n", read(t, filepath.Join(dir, "f.py")))
	})
}

func TestApply_Errors(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.txt"), "one\ntwo\n")
	_, err := apply(t, dir, "*** Update File: a.txt\n@@\n-three\n+four")
	require.Error(t, err)
	assert.Equal(t, "Failed to find expected lines in "+filepath.Join(dir, "a.txt")+":\nthree", err.Error())

	_, err = apply(t, dir, "*** Update File: a.txt\n@@ def missing():\n-one\n+uno")
	require.Error(t, err)
	assert.Equal(t, "Failed to find context 'def missing():' in "+filepath.Join(dir, "a.txt"), err.Error())

	_, err = apply(t, dir, "*** Update File: missing.txt\n@@\n-x\n+y")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Failed to read file to update "+filepath.Join(dir, "missing.txt"))

	_, err = apply(t, dir, "*** Delete File: missing.txt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Failed to delete file")

	_, err = patch.Compute(dir, nil)
	require.EqualError(t, err, "No files were modified.")
	assert.Equal(t, "one\ntwo\n", read(t, filepath.Join(dir, "a.txt")), "a failed patch writes nothing")
}

func TestApply_LaterHunksSeeEarlierOnes(t *testing.T) {
	dir := t.TempDir()
	_, err := apply(t, dir, "*** Add File: new.txt\n+a\n*** Update File: new.txt\n@@\n-a\n+b")
	require.NoError(t, err)
	assert.Equal(t, "b\n", read(t, filepath.Join(dir, "new.txt")))
}

func TestTool_HookInputRoundTrip(t *testing.T) {
	args := `{"input":"*** Begin Patch\n*** Add File: a.txt\n+x\n*** End Patch"}`
	assert.JSONEq(t, `{"command":"*** Begin Patch\n*** Add File: a.txt\n+x\n*** End Patch","file_path":"a.txt","file_paths":["a.txt"]}`, string(patch.HookInput(args)))
	back, err := patch.FromHookInput([]byte(`{"command":"P"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"input":"P"}`, back)
	assert.Equal(t, "a.txt", patch.Describe(args))
}

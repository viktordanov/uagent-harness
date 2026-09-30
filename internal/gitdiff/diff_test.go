package gitdiff_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/gitdiff"
	"github.com/viktordanov/uah/internal/patch"
)

// repo is a temporary git repository, isolated from the user's and the
// system's git configuration.
type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	r := &repo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main")

	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = r.dir
	out, err := cmd.CombinedOutput()
	require.NoError(r.t, err, string(out))

	return strings.TrimSpace(string(out))
}

func (r *repo) write(name, text string) {
	r.t.Helper()
	path := filepath.Join(r.dir, name)
	require.NoError(r.t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(r.t, os.WriteFile(path, []byte(text), 0o644))
}

func (r *repo) commit(msg string) string {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)

	return r.git("rev-parse", "HEAD")
}

func file(t *testing.T, d gitdiff.Diff, path string) gitdiff.File {
	t.Helper()
	i := slices.IndexFunc(d.Files, func(f gitdiff.File) bool { return f.Path == path })
	require.GreaterOrEqual(t, i, 0, "no %s in %+v", path, d.Files)

	return d.Files[i]
}

func lines(f gitdiff.File) []patch.DiffLine {
	var out []patch.DiffLine
	for _, h := range f.Hunks {
		out = append(out, h.Lines...)
	}

	return out
}

// TestCollect_StagedUnstagedUntracked shows every kind of change once:
// an unstaged edit, a staged new file, a deletion, a rename, and untracked
// text, binary, and large files, from a subdirectory of the work tree.
func TestCollect_StagedUnstagedUntracked(t *testing.T) {
	r := newRepo(t)
	r.write("a.go", "package a\n\nfunc A() int {\n\treturn 1\n}\n")
	r.write("gone.txt", "bye\n")
	r.write("old.txt", "one\ntwo\nthree\nfour\nfive\n")
	r.write("sub/keep.txt", "keep\n")
	r.write(".gitignore", "ignored.log\n")
	r.commit("first")

	r.write("a.go", "package a\n\nfunc A() int {\n\treturn 2\n}\n") // unstaged
	r.write("staged.go", "package a\n")
	r.git("add", "staged.go") // staged
	r.git("rm", "-q", "gone.txt")
	r.git("mv", "old.txt", "new.txt")
	r.write("notes.md", "first\nsecond\n")
	r.write("blob.bin", "PNG\x00\x01\x02")
	r.write("big.txt", strings.Repeat("x", 600<<10))
	r.write("ignored.log", "noise\n")

	d, err := gitdiff.Collect(context.Background(), filepath.Join(r.dir, "sub"))
	require.NoError(t, err)

	realRoot, err := filepath.EvalSymlinks(r.dir)
	require.NoError(t, err)
	assert.Equal(t, realRoot, d.Root)

	a := file(t, d, "a.go")
	assert.Equal(t, "update", a.Op)
	assert.Equal(t, 1, a.Added)
	assert.Equal(t, 1, a.Removed)
	assert.Contains(t, lines(a), patch.DiffLine{Kind: "-", Old: 4, Text: "\treturn 1"})
	assert.Contains(t, lines(a), patch.DiffLine{Kind: "+", New: 4, Text: "\treturn 2"})

	staged := file(t, d, "staged.go")
	assert.Equal(t, "add", staged.Op)
	assert.False(t, staged.Untracked)
	assert.Equal(t, 1, staged.Added)

	assert.Equal(t, "delete", file(t, d, "gone.txt").Op)
	renamed := file(t, d, "old.txt")
	assert.Equal(t, "new.txt", renamed.MovePath)

	notes := file(t, d, "notes.md")
	assert.True(t, notes.Untracked)
	assert.Equal(t, []patch.DiffLine{{Kind: "+", New: 1, Text: "first"}, {Kind: "+", New: 2, Text: "second"}}, lines(notes))
	assert.Equal(t, gitdiff.NoteBinary, file(t, d, "blob.bin").Note)
	assert.Contains(t, file(t, d, "big.txt").Note, gitdiff.NoteLarge)

	for _, f := range d.Files {
		assert.NotEqual(t, "ignored.log", f.Path, "ignored files stay out")
	}
	assert.Equal(t, 4, d.Added(), "a.go, staged.go, and notes.md")
	assert.Equal(t, 2, d.Removed(), "a.go and gone.txt")
}

// TestCollect_TrackedBinary notes a changed binary file git tracks.
func TestCollect_TrackedBinary(t *testing.T) {
	r := newRepo(t)
	r.write("img.bin", "\x00\x01")
	r.commit("first")
	r.write("img.bin", "\x00\x02\x03")

	d, err := gitdiff.Collect(context.Background(), r.dir)
	require.NoError(t, err)
	require.Len(t, d.Files, 1)
	assert.Equal(t, "img.bin", d.Files[0].Path)
	assert.Equal(t, gitdiff.NoteBinary, d.Files[0].Note)
}

// TestCollect_BeforeTheFirstCommit diffs against the empty tree.
func TestCollect_BeforeTheFirstCommit(t *testing.T) {
	r := newRepo(t)
	r.write("staged.txt", "a\n")
	r.git("add", "staged.txt")
	r.write("loose.txt", "b\n")

	d, err := gitdiff.Collect(context.Background(), r.dir)
	require.NoError(t, err)
	assert.Equal(t, "add", file(t, d, "staged.txt").Op)
	assert.True(t, file(t, d, "loose.txt").Untracked)
}

func TestCollect_CleanTree(t *testing.T) {
	r := newRepo(t)
	r.write("a.txt", "a\n")
	r.commit("first")

	d, err := gitdiff.Collect(context.Background(), r.dir)
	require.NoError(t, err)
	assert.Empty(t, d.Files)
	assert.Zero(t, d.MoreUntracked)
}

func TestCollect_NotARepo(t *testing.T) {
	t.Setenv("GIT_CEILING_DIRECTORIES", os.TempDir())
	_, err := gitdiff.Collect(context.Background(), t.TempDir())
	require.ErrorIs(t, err, gitdiff.ErrNotRepo)
	_, err = gitdiff.ListBranches(context.Background(), t.TempDir())
	require.ErrorIs(t, err, gitdiff.ErrNotRepo)
}

// TestRefs lists the branches with the default first, the recent
// commits newest first, and a branch's merge base.
func TestRefs(t *testing.T) {
	r := newRepo(t)
	r.write("a.txt", "a\n")
	base := r.commit("first")
	r.git("checkout", "-q", "-b", "feature")
	r.write("a.txt", "b\n")
	head := r.commit("second")
	r.git("branch", "alpha", base)

	b, err := gitdiff.ListBranches(context.Background(), r.dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"main", "alpha", "feature"}, b.Names)
	assert.Equal(t, "feature", b.Current)

	commits, err := gitdiff.ListCommits(context.Background(), r.dir, gitdiff.RecentCommits)
	require.NoError(t, err)
	assert.Equal(t, []gitdiff.Commit{{SHA: head, Subject: "second"}, {SHA: base, Subject: "first"}}, commits)

	mb, err := gitdiff.MergeBase(context.Background(), r.dir, "main")
	require.NoError(t, err)
	assert.Equal(t, base, mb)
	mb, err = gitdiff.MergeBase(context.Background(), r.dir, "no-such-branch")
	require.NoError(t, err)
	assert.Empty(t, mb, "no merge base: the prompt asks the reviewer to find it")
}

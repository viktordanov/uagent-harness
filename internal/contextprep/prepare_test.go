package contextprep_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/contextprep"
)

// fixed is an adapter with a fixed block.
type fixed struct {
	name, text string
	limit      int
}

func (a fixed) Name() string                                      { return a.name }
func (a fixed) Prepare(context.Context, contextprep.Facts) string { return a.text }

// limited is a fixed adapter with its own cap.
type limited struct{ fixed }

func (a limited) MaxBytes() int { return a.limit }

func TestPrepare(t *testing.T) {
	ctx := t.Context()
	assert.Empty(t, contextprep.Prepare(ctx, contextprep.Facts{}, fixed{name: "a"}, fixed{name: "b", text: "  \n"}), "nothing to say")

	got := contextprep.Prepare(ctx, contextprep.Facts{}, fixed{name: "one", text: "first\n"}, fixed{name: "empty"}, fixed{name: "two", text: "second"})
	assert.True(t, contextprep.IsPrepared(got), got)
	assert.True(t, strings.HasSuffix(got, "\n"+contextprep.Close), got)
	assert.Contains(t, got, "\n\n## one\nfirst\n\n## two\nsecond\n")
	assert.NotContains(t, got, "## empty")
	assert.False(t, contextprep.IsPrepared("hello"))

	long := strings.Repeat("line of text\n", 1000)
	got = contextprep.Prepare(ctx, contextprep.Facts{}, fixed{name: "long", text: long}, fixed{name: "after", text: "kept"})
	assert.Contains(t, got, "(cut: the rest is over 4096 bytes)")
	assert.Contains(t, got, "## after\nkept")
	assert.Less(t, len(got), 4096+500)

	got = contextprep.Prepare(ctx, contextprep.Facts{}, limited{fixed{name: "big", text: long, limit: 8 << 10}})
	assert.Contains(t, got, "(cut: the rest is over 8192 bytes)", "an adapter's own cap")
	assert.Greater(t, len(got), 8000)

	var many []contextprep.Adapter
	for range 6 {
		many = append(many, limited{fixed{name: "big", text: long, limit: 8 << 10}})
	}
	got = contextprep.Prepare(ctx, contextprep.Facts{}, many...)
	assert.LessOrEqual(t, len(got), contextprep.MaxBytes)
	assert.Equal(t, 2, strings.Count(got, "## big"), "the second is cut to the room left, the rest left out")
}

func TestWorkspace(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	ws := t.TempDir()
	assert.Empty(t, contextprep.Workspace{}.Prepare(t.Context(), contextprep.Facts{Workspace: ws}), "not a repository")

	require.NoError(t, os.MkdirAll(filepath.Join(ws, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, "pkg", "a.go"), []byte("package pkg\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(ws, "README.md"), []byte("hi\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(ws, "new.txt"), []byte("new\n"), 0o644))
	for _, args := range [][]string{{"init", "-q", "-b", "trunk"}, {"add", "pkg", "README.md"}} {
		out, err := exec.Command("git", append([]string{"-C", ws}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	got := contextprep.Workspace{}.Prepare(t.Context(), contextprep.Facts{Workspace: ws})
	assert.Contains(t, got, "Git branch: trunk")
	assert.Contains(t, got, "A  README.md")
	assert.Contains(t, got, "?? new.txt")
	assert.Contains(t, got, "Files (git ls-files: 2 in all):\nREADME.md\npkg/ (1 files)")
}

func TestHarness(t *testing.T) {
	got := contextprep.Harness{}.Prepare(t.Context(), contextprep.Facts{})
	assert.Contains(t, got, "max_output_length")
	assert.Contains(t, got, "default 40000")
}

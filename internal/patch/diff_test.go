package patch_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/patch"
)

// render writes a diff as "12 +text" lines, hunks separated by "⋮".
func render(d patch.FileDiff) string {
	var b strings.Builder
	for i, h := range d.Hunks {
		if i > 0 {
			b.WriteString("⋮\n")
		}
		for _, l := range h.Lines {
			fmt.Fprintf(&b, "%02d %s%s\n", l.Line(), l.Kind, l.Text)
		}
	}

	return b.String()
}

func TestDiff_Update(t *testing.T) {
	d := patch.Diff(patch.Change{
		Op: patch.Update, Path: "a.txt",
		Old: "1\n2\n3\n4\n5\n6\n7\n8\n9\n",
		New: "1\ntwo\n3\n4\n5\n6\n7\n8\nnine\nten\n",
	})
	assert.Equal(t, "update", d.Op)
	assert.Equal(t, 3, d.Added)
	assert.Equal(t, 2, d.Removed)
	assert.Equal(t, "01  1\n02 -2\n02 +two\n03  3\n⋮\n08  8\n09 -9\n09 +nine\n10 +ten\n", render(d))
}

func TestDiff_AddAndDelete(t *testing.T) {
	add := patch.Diff(patch.Change{Op: patch.Add, Path: "n.txt", New: "a\nb\n"})
	assert.Equal(t, 2, add.Added)
	assert.Equal(t, "01 +a\n02 +b\n", render(add))
	del := patch.Diff(patch.Change{Op: patch.Delete, Path: "n.txt", Old: "a\n"})
	assert.Equal(t, 1, del.Removed)
	assert.Equal(t, "01 -a\n", render(del))
}

func TestDiff_FromAppliedPatch(t *testing.T) {
	dir := t.TempDir()
	write(t, dir+"/f.go", "package f\n\nfunc A() int {\n\treturn 1\n}\n")
	hunks, err := patch.Parse("*** Begin Patch\n*** Update File: f.go\n@@ func A() int {\n-\treturn 1\n+\treturn 2\n*** End Patch")
	require.NoError(t, err)
	changes, err := patch.Compute(dir, hunks)
	require.NoError(t, err)
	d := patch.Diffs(changes)
	require.Len(t, d, 1)
	assert.Equal(t, "03  func A() int {\n04 -\treturn 1\n04 +\treturn 2\n05  }\n", render(d[0]))
}

func TestDiff_LargeRewriteStaysBounded(t *testing.T) {
	var a, b strings.Builder
	for i := range 5000 {
		a.WriteString("old " + strings.Repeat("x", i%7) + "\n")
		b.WriteString("new " + strings.Repeat("y", i%5) + "\n")
	}
	d := patch.Diff(patch.Change{Op: patch.Update, Old: a.String(), New: b.String()})
	assert.Equal(t, 5000, d.Added)
	assert.Equal(t, 5000, d.Removed)
	assert.Positive(t, d.Omitted)
}

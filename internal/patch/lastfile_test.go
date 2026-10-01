package patch_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uah/internal/patch"
)

// TestLastFile: the file of the last complete header, in a raw or a
// JSON-escaped patch, and none while the path is still being written.
func TestLastFile(t *testing.T) {
	assert.Equal(t, "b.go", patch.LastFile("*** Update File: a.go\n@@\n*** Add File: b.go\n+x", false))
	assert.Equal(t, "dir/c.go", patch.LastFile(`{"input":"*** Begin Patch\n*** Delete File: dir/c.go\n`, false))
	assert.Equal(t, "d.go", patch.LastFile(`"*** Add File: d.go"`, false))
	assert.Empty(t, patch.LastFile("*** Update File: half", false))
	assert.Empty(t, patch.LastFile("no header", false))
}

// TestLastFileRaw: a freeform call's raw patch ends a path only at the
// line's end, so quotes and backslashes stay in it.
func TestLastFileRaw(t *testing.T) {
	assert.Equal(t, "b.go", patch.LastFile("*** Begin Patch\n*** Update File: a.go\n@@\n*** Add File: b.go\n+x", true))
	assert.Equal(t, `say "hi"\x.txt`, patch.LastFile("*** Add File: say \"hi\"\\x.txt\n", true))
	assert.Empty(t, patch.LastFile("*** Update File: half", true))
	assert.Empty(t, patch.LastFile("no header", true))
}

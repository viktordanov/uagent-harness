package patch_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uah/internal/patch"
)

// TestLastFile: the file of the last complete header, which ends only at
// the line's end, so quotes and backslashes stay in it, and none while the
// path is still being written.
func TestLastFile(t *testing.T) {
	assert.Equal(t, "b.go", patch.LastFile("*** Begin Patch\n*** Update File: a.go\n@@\n*** Add File: b.go\n+x"))
	assert.Equal(t, "dir/c.go", patch.LastFile("*** Begin Patch\n*** Delete File: dir/c.go\n"))
	assert.Equal(t, `say "hi"\x.txt`, patch.LastFile("*** Add File: say \"hi\"\\x.txt\n"))
	assert.Empty(t, patch.LastFile("*** Update File: half"))
	assert.Empty(t, patch.LastFile("no header"))
}

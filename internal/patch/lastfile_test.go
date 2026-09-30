package patch_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uah/internal/patch"
)

// TestLastFile: the file of the last complete header, in a raw or a
// JSON-escaped patch, and none while the path is still being written.
func TestLastFile(t *testing.T) {
	assert.Equal(t, "b.go", patch.LastFile("*** Update File: a.go\n@@\n*** Add File: b.go\n+x"))
	assert.Equal(t, "dir/c.go", patch.LastFile(`{"input":"*** Begin Patch\n*** Delete File: dir/c.go\n`))
	assert.Equal(t, "d.go", patch.LastFile(`"*** Add File: d.go"`))
	assert.Empty(t, patch.LastFile("*** Update File: half"))
	assert.Empty(t, patch.LastFile("no header"))
}

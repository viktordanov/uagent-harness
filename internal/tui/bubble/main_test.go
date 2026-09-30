package bubble_test

import (
	"os"
	"testing"

	"github.com/viktordanov/uah/testing/harnesstest"
)

// TestMain keeps every test away from the user's ~/.uah. Run as the
// editor of an editor test, the test binary edits the file it is given
// instead (editor_test.go).
func TestMain(m *testing.M) {
	if os.Getenv(fakeEditorEnv) != "" {
		os.Exit(fakeEditor(os.Args[len(os.Args)-1]))
	}
	os.Exit(harnesstest.IsolatedMain(m))
}

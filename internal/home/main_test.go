package home_test

import (
	"os"
	"testing"

	"github.com/viktordanov/uah/testing/harnesstest"
)

// TestMain keeps every test away from the user's ~/.uah.
func TestMain(m *testing.M) { os.Exit(harnesstest.IsolatedMain(m)) }

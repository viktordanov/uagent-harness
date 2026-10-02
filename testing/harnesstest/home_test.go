package harnesstest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/home"
	"github.com/viktordanov/uah/testing/harnesstest"
)

var realHome = os.Getenv("HOME")

func TestMain(m *testing.M) { os.Exit(harnesstest.IsolatedMain(m)) }

// TestIsolatedMain: UAH_HOME, HOME, and CODEX_HOME are in one temporary
// directory, not the user's home, and the variables uah reads are clear.
func TestIsolatedMain(t *testing.T) {
	dir := filepath.Dir(os.Getenv(home.Env))
	require.True(t, strings.HasPrefix(filepath.Base(dir), "uah-home"), "UAH_HOME is in the temporary directory: %s", dir)
	userHome, err := os.UserHomeDir()
	require.NoError(t, err)
	for name, got := range map[string]string{"HOME": userHome, "CODEX_HOME": os.Getenv("CODEX_HOME")} {
		assert.True(t, strings.HasPrefix(got, dir+string(filepath.Separator)), "%s is in %s: %s", name, dir, got)
		if realHome != "" {
			assert.NotEqual(t, realHome, got, name)
		}
	}
	for _, name := range home.Variables {
		_, ok := os.LookupEnv(name)
		assert.False(t, ok, name)
	}
}

package harnesstest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/viktordanov/uah/internal/home"
)

// IsolatedMain runs a package's tests with uah's home in a temporary
// directory and without the variables that change what uah does
// (home.Variables), so no test reads the user's ~/.uah (their
// configuration, layers, hooks, or trust) or takes their settings.
// Tests that need another home still set UAH_HOME. Call it from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(harnesstest.IsolatedMain(m)) }
func IsolatedMain(m *testing.M) int {
	dir, err := os.MkdirTemp("", "uah-home")
	if err != nil {
		fmt.Fprintln(os.Stderr, "isolate uah's home:", err)

		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := isolate(filepath.Join(dir, "home")); err != nil {
		fmt.Fprintln(os.Stderr, "isolate uah's home:", err)

		return 1
	}

	return m.Run()
}

func isolate(homeDir string) error {
	if err := os.Setenv(home.Env, homeDir); err != nil {
		return fmt.Errorf("failed to set %s: %w", home.Env, err)
	}
	for _, name := range home.Variables {
		if err := os.Unsetenv(name); err != nil {
			return fmt.Errorf("failed to unset %s: %w", name, err)
		}
	}

	return nil
}

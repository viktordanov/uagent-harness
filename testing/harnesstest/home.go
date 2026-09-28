package harnesstest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// homeVariables move uah's files; IsolatedMain clears them. UAH_HOME is set
// to a temporary directory instead.
var homeVariables = []string{"UAH_CONFIG", "UAH_STATE_DIR", "UAH_EXTRA_CONFIG", "UAGENT_CONFIG", "UAGENT_STATE_DIR"}

// IsolatedMain runs a package's tests with uah's home in a temporary
// directory and without the variables that move uah's files, so no test
// reads the user's ~/.uah: their configuration, layers, hooks, or trust.
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
	if err := os.Setenv("UAH_HOME", homeDir); err != nil {
		return fmt.Errorf("failed to set UAH_HOME: %w", err)
	}
	for _, name := range homeVariables {
		if err := os.Unsetenv(name); err != nil {
			return fmt.Errorf("failed to unset %s: %w", name, err)
		}
	}

	return nil
}

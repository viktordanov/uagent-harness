package harnesstest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/viktordanov/uah/internal/home"
)

// IsolatedMain runs a package's tests with uah's home, HOME, and
// CODEX_HOME in a temporary directory and without the variables that
// change what uah does (home.Variables), so no test reads the user's ~/.uah
// (their configuration, layers, hooks, or trust), their ~/.codex (its
// AGENTS.md or skills), or takes their settings. Tests that need another
// home still set UAH_HOME, HOME, or CODEX_HOME. Call it from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(harnesstest.IsolatedMain(m)) }
func IsolatedMain(m *testing.M) int {
	dir, err := os.MkdirTemp("", "uah-home")
	if err != nil {
		fmt.Fprintln(os.Stderr, "isolate uah's home:", err)

		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := Isolate(dir); err != nil {
		fmt.Fprintln(os.Stderr, "isolate uah's home:", err)

		return 1
	}

	return m.Run()
}

// Isolate points UAH_HOME, HOME, and CODEX_HOME into dir and clears
// home.Variables and the XDG folders uah reads, for a TestMain that does
// more than IsolatedMain. Go's own folders keep their real places, so the
// go commands tests run still find the module and build caches.
func Isolate(dir string) error {
	if err := pinGoEnv(); err != nil {
		return err
	}
	userHome := filepath.Join(dir, "user")
	set := map[string]string{
		home.Env:     filepath.Join(dir, "home"),
		"HOME":       userHome,
		"CODEX_HOME": filepath.Join(userHome, ".codex"),
	}
	for name, value := range set {
		if err := os.Setenv(name, value); err != nil {
			return fmt.Errorf("failed to set %s: %w", name, err)
		}
	}
	for _, name := range append([]string{"XDG_CONFIG_HOME", "XDG_STATE_HOME"}, home.Variables...) {
		if err := os.Unsetenv(name); err != nil {
			return fmt.Errorf("failed to unset %s: %w", name, err)
		}
	}

	return nil
}

// pinGoEnv sets Go's folders, which default to places under HOME, to where
// they are now. Without go on PATH there is nothing to pin.
func pinGoEnv() error {
	names := []string{"GOENV", "GOPATH", "GOMODCACHE", "GOCACHE"}
	out, err := exec.CommandContext(context.Background(), "go", append([]string{"env", "-json"}, names...)...).Output()
	if err != nil {
		return nil //nolint:nilerr // no go, so no go command to keep working
	}
	var env map[string]string
	if err := json.Unmarshal(out, &env); err != nil {
		return fmt.Errorf("failed to read go env: %w", err)
	}
	for _, name := range names {
		if env[name] == "" {
			continue
		}
		if err := os.Setenv(name, env[name]); err != nil {
			return fmt.Errorf("failed to set %s: %w", name, err)
		}
	}

	return nil
}

// realCache is the user's cache directory, read before Isolate moves HOME
// into a temporary directory.
var realCache, errRealCache = os.UserCacheDir()

// OutsideDir makes a directory, removed after the test, that is outside
// every root the sandbox lets a command write: one in the user's real cache
// directory, since the isolated HOME is under the temporary directory the
// sandbox allows.
func OutsideDir(tb testing.TB, pattern string) string {
	tb.Helper()
	if errRealCache != nil {
		tb.Fatalf("find the user's cache directory: %v", errRealCache)
	}
	if err := os.MkdirAll(realCache, 0o700); err != nil {
		tb.Fatalf("create the user's cache directory: %v", err)
	}
	dir, err := os.MkdirTemp(realCache, pattern) //nolint:usetesting // tb.TempDir is in the temporary directory the sandbox allows
	if err != nil {
		tb.Fatalf("create a directory outside the sandbox: %v", err)
	}
	tb.Cleanup(func() { _ = os.RemoveAll(dir) })

	return dir
}

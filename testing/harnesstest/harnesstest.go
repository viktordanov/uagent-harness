// Package harnesstest sets up isolated uagent environments for tests: the
// fake runner from uagent, a workspace, a state directory, and Codex
// credentials that pass preflight.
package harnesstest

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var (
	buildOnce sync.Once
	buildPath string
	errBuild  error
)

// FakeRunner builds uagent's fake runner once per test binary and returns its path.
func FakeRunner(tb testing.TB) string {
	tb.Helper()
	buildOnce.Do(func() {
		// Shared by every test in the binary, so not tb.TempDir, which the first test would remove.
		dir, err := os.MkdirTemp("", "uah-fakerunner") //nolint:usetesting // see above
		if err != nil {
			errBuild = err

			return
		}
		buildPath = filepath.Join(dir, "fakerunner")
		build := exec.CommandContext(context.Background(), "go", "build", "-o", buildPath, "github.com/viktordanov/uagent/testing/fakerunner")
		out, err := build.CombinedOutput()
		if err != nil {
			errBuild = fmt.Errorf("build fakerunner: %w\n%s", err, out)
		}
	})
	require.NoError(tb, errBuild)

	return buildPath
}

// Env is one isolated setup.
type Env struct {
	StateDir  string
	Workspace string
	CodexHome string
	Capture   string
}

// NewEnv creates the directories and a Codex token valid for a day.
func NewEnv(tb testing.TB) *Env {
	tb.Helper()
	root := tb.TempDir()
	e := &Env{
		StateDir:  filepath.Join(root, "state"),
		Workspace: filepath.Join(root, "workspace"),
		CodexHome: filepath.Join(root, "codex"),
		Capture:   filepath.Join(root, "capture"),
	}
	for _, dir := range []string{e.Workspace, e.CodexHome, e.Capture} {
		require.NoError(tb, os.MkdirAll(dir, 0o700))
	}
	claims := `{"exp":` + strconv.FormatInt(time.Now().Add(24*time.Hour).Unix(), 10) + `}`
	auth := `{"tokens":{"access_token":"x.` + base64.RawURLEncoding.EncodeToString([]byte(claims)) + `.y"}}`
	require.NoError(tb, os.WriteFile(filepath.Join(e.CodexHome, "auth.json"), []byte(auth), 0o600))

	return e
}

// Getenv serves CODEX_HOME for harness.Config.Getenv.
func (e *Env) Getenv(key string) string {
	if key == "CODEX_HOME" {
		return e.CodexHome
	}

	return ""
}

package main_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/testing/fixtures"

	"github.com/viktordanov/uagent-harness/testing/harnesstest"
)

var uahBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "uah-cli")
	if err != nil {
		panic(err)
	}
	uahBin = filepath.Join(dir, "uah")
	if msg, err := exec.Command("go", "build", "-o", uahBin, ".").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build uah: %v\n%s", err, msg)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type cliResult struct {
	code           int
	stdout, stderr string
}

func uah(t *testing.T, args ...string) cliResult {
	t.Helper()

	return uahWith(t, nil, "", args...)
}

// uahWith runs uah with extra environment variables and stdin.
func uahWith(t *testing.T, env []string, stdin string, args ...string) cliResult {
	t.Helper()
	cmd := exec.Command(uahBin, args...)
	cmd.Env = append(os.Environ(), env...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		code = exitErr.ExitCode()
	} else {
		require.NoError(t, err)
	}

	return cliResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func TestVersion(t *testing.T) {
	for _, flag := range []string{"--version", "-v"} {
		res := uah(t, flag)
		assert.Equal(t, 0, res.code, flag)
		assert.Contains(t, res.stdout, "uah version", flag)
	}
}

func TestHelpListsCommands(t *testing.T) {
	res := uah(t, "--help")
	require.Equal(t, 0, res.code)
	assert.Contains(t, res.stdout, "run")
	assert.Contains(t, res.stdout, "sessions")
	assert.Contains(t, res.stdout, "a general-purpose harness for unreal-agent-runner")
}

func TestUnknownFlag(t *testing.T) {
	res := uah(t, "--no-such-flag")
	assert.Equal(t, 2, res.code)
	assert.Empty(t, res.stdout)
	assert.Equal(t, 1, strings.Count(res.stderr, "\n"), res.stderr)
	assert.Contains(t, res.stderr, "no-such-flag")
}

func TestTUINotImplementedYet(t *testing.T) {
	res := uah(t)
	assert.Equal(t, 1, res.code)
	assert.Contains(t, res.stderr, "not implemented yet")
}

// fakeEnv points uah at the fake runner replaying fixture, in a fresh state dir.
func fakeEnv(t *testing.T, fixture string) (*harnesstest.Env, []string) {
	t.Helper()
	e := harnesstest.NewEnv(t)

	return e, []string{
		"UAGENT_RUNNER=" + harnesstest.FakeRunner(t),
		"UAGENT_STATE_DIR=" + e.StateDir,
		"CODEX_HOME=" + e.CodexHome,
		"FAKERUNNER_FIXTURE=" + fixtures.Path(fixture),
		"FAKERUNNER_ECHO=1",
		"UNREAL_HARNESS_LLM_PROVIDER=",
		"UNREAL_HARNESS_LLM_MODEL=",
	}
}

func TestRunAndSessions(t *testing.T) {
	e, env := fakeEnv(t, "simple.jsonl")

	first := uahWith(t, env, "", "run", "-C", e.Workspace, "first question")
	require.Equal(t, 0, first.code, first.stderr)
	assert.Equal(t, "hello\n", first.stdout)
	assert.Contains(t, first.stderr, "› first question")
	assert.Contains(t, first.stderr, "run ok")

	list := uahWith(t, env, "", "sessions")
	require.Equal(t, 0, list.code, list.stderr)
	lines := strings.Split(strings.TrimSpace(list.stdout), "\n")
	require.Len(t, lines, 2, list.stdout)
	id := strings.Fields(lines[1])[0]
	assert.Contains(t, lines[1], "first question")

	env = append(env, "FAKERUNNER_FIXTURE="+fixtures.Path("parallel.jsonl"))
	more := uahWith(t, env, "second\nthird\n", "run", "--session", id, "--stdin")
	require.Equal(t, 0, more.code, more.stderr)
	assert.Contains(t, more.stdout, "A; B")
	assert.Contains(t, more.stderr, "(resumed)")

	show := uahWith(t, env, "", "sessions", "show", id)
	require.Equal(t, 0, show.code, show.stderr)
	assert.Contains(t, show.stdout, "› first question")
	assert.Contains(t, show.stdout, "✓ hello")
	assert.Contains(t, show.stdout, "› second")
	assert.Contains(t, show.stdout, "✓ A; B")

	missing := uahWith(t, env, "", "sessions", "show", "zzzz")
	assert.Equal(t, 2, missing.code)
	assert.Contains(t, missing.stderr, "no session matches")
}

func TestRunStream(t *testing.T) {
	e, env := fakeEnv(t, "simple.jsonl")

	res := uahWith(t, env, "", "run", "--stream", "-C", e.Workspace, "hi")

	require.Equal(t, 0, res.code, res.stderr)
	var types []string
	for line := range strings.Lines(res.stdout) {
		var ev struct {
			Type string `json:"type"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &ev), line)
		types = append(types, ev.Type)
	}
	require.NotEmpty(t, types)
	assert.Equal(t, "session_opened", types[0])
	assert.Equal(t, "idle", types[len(types)-1])
	for _, want := range []string{"input_queued", "input_sent", "run_started", "user_message", "input_delivered", "run_finished"} {
		assert.Contains(t, types, want)
	}
}

func TestRunFailures(t *testing.T) {
	t.Run("no prompt", func(t *testing.T) {
		_, env := fakeEnv(t, "simple.jsonl")
		res := uahWith(t, env, "", "run")
		assert.Equal(t, 2, res.code)
	})

	t.Run("a blocked preflight fails the run", func(t *testing.T) {
		e, env := fakeEnv(t, "simple.jsonl")
		require.NoError(t, os.WriteFile(filepath.Join(e.Workspace, ".env"), []byte("UNREAL_HARNESS_LLM_BASE_URL=http://evil\n"), 0o600))

		res := uahWith(t, env, "", "run", "-C", e.Workspace, "hi")

		assert.Equal(t, 1, res.code)
		assert.Contains(t, res.stderr, "preflight blocked the run")
	})

	t.Run("an invalid effort is a usage error", func(t *testing.T) {
		_, env := fakeEnv(t, "simple.jsonl")
		res := uahWith(t, env, "", "run", "-e", "huge", "hi")
		assert.Equal(t, 2, res.code)
	})
}

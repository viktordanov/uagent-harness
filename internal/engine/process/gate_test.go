package process_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"
	uaharness "github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/engine/process"
	"github.com/viktordanov/uagent-harness/internal/engine/process/shellgate"
	"github.com/viktordanov/uagent-harness/internal/rules"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
	"github.com/viktordanov/uagent-harness/testing/harnesstest"
)

// TestMain lets the test binary serve as the shell gate, as uah does.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == shellgate.Command {
		os.Exit(shellgate.Main(os.Args[2:], os.Stderr))
	}
	os.Exit(m.Run())
}

func rule(decision rules.Decision, justification string, words ...string) rules.Rule {
	pattern := make([][]string, 0, len(words))
	for _, w := range words {
		pattern = append(pattern, []string{w})
	}

	return rules.Rule{Pattern: pattern, Decision: decision, Justification: justification}
}

var testRules = []rules.Rule{
	rule(rules.Forbidden, "never delete", "rm"),
	rule(rules.Prompt, "", "git", "push"),
	rule(rules.Allow, "", "touch"),
}

// shells are the test binary's gate shells for the policy.
func shells(t *testing.T, dir string, p sandbox.Policy, list []rules.Rule) process.Shells {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)

	return process.Shells{Dir: dir, Policy: p, Real: "/bin/sh", Gate: exe, Rules: list}
}

// sh runs the shell as the runner does: shell -c command.
func sh(t *testing.T, shell, dir, command string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), shell, "-c", command)
	cmd.Dir = dir
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		code = exit.ExitCode()
	} else {
		require.NoError(t, err)
	}

	return out.String(), errOut.String(), code
}

// TestProcess_ShellAppliesRules runs the gate shell as the runner would:
// a forbidden command and a prompt one are refused with the reason the
// embedded engine gives, an allowed one runs outside the sandbox, and the
// rest run in it.
func TestProcess_ShellAppliesRules(t *testing.T) {
	dir, workspace := t.TempDir(), t.TempDir()
	policy := sandbox.Policy{Mode: sandbox.ReadOnly, Workspace: workspace}
	gated := shells(t, dir, policy, testRules)
	require.True(t, gated.Gated())
	shell, unavailable, err := gated.For(sandbox.ReadOnly)
	require.NoError(t, err)

	_, stderr, code := sh(t, shell, workspace, "rm -rf "+workspace)
	assert.Equal(t, 1, code)
	assert.Equal(t, "not run: a rule forbids this command: never delete.\n", stderr)
	assert.DirExists(t, workspace)

	_, stderr, code = sh(t, shell, workspace, "git push origin main")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "not run: this command needs the user's approval, and no user can approve it")

	stdout, _, code := sh(t, shell, workspace, "echo hi && echo there")
	assert.Equal(t, 0, code)
	assert.Equal(t, "hi\nthere\n", stdout, "a command no rule matches runs")

	allowed := filepath.Join(workspace, "allowed")
	_, stderr, code = sh(t, shell, workspace, "touch "+allowed)
	assert.Equal(t, 0, code, stderr)
	assert.FileExists(t, allowed, "an allow rule runs the command outside the read-only sandbox")
	if !unavailable {
		_, _, code = sh(t, shell, workspace, "echo x > "+filepath.Join(workspace, "blocked"))
		assert.NotEqual(t, 0, code, "anything else stays in the read-only sandbox")
		assert.NoFileExists(t, filepath.Join(workspace, "blocked"))
	}

	plain, _, err := shells(t, dir, policy, nil).For(sandbox.ReadOnly)
	require.NoError(t, err)
	script, err := os.ReadFile(plain) //nolint:gosec // the test's own script
	if err == nil {
		assert.NotContains(t, string(script), shellgate.Command, "without rules there is no gate")
	}
}

// TestProcess_RealRunnerAppliesRules runs the real runner on the process
// engine with the gate: the model's forbidden and prompt commands are not
// run and it hears why, and the others run.
func TestProcess_RealRunnerAppliesRules(t *testing.T) {
	if testing.Short() {
		t.Skip("builds unreal-agent-runner")
	}
	env := harnesstest.NewEnv(t)
	llm := fakellm.New(t,
		fakellm.Reply{Text: "Working.", Commands: []string{"rm -rf keep", "git push origin main", "touch made"}},
		fakellm.Reply{Text: "done"},
	)
	require.NoError(t, os.WriteFile(filepath.Join(env.Workspace, "keep"), []byte("x"), 0o600))
	t.Setenv("OPENAI_API_KEY", "test-key")
	getenv := func(key string) string {
		if key == "OPENAI_API_KEY" {
			return "test-key"
		}

		return env.Getenv(key)
	}
	gated := shells(t, filepath.Join(env.StateDir, "sandbox"), sandbox.Policy{Mode: sandbox.WorkspaceWrite, Workspace: env.Workspace}, testRules)
	eng, err := process.NewSandboxed(sandbox.WorkspaceWrite, true, func(mode sandbox.Mode) (uaharness.Config, error) {
		shell, _, err := gated.For(mode)
		backend := uaharness.RunnerBackend{Path: harnesstest.RealRunner(t), Env: []string{"SHELL=" + shell}}

		return uaharness.Config{Backend: backend, StateDir: env.StateDir, Getenv: getenv, KillGrace: time.Second}, err
	})
	require.NoError(t, err)
	assert.True(t, eng.Capabilities().Rules)

	settings := session.Settings{Provider: "openai", Model: "gpt-test", Effort: "high", Workspace: env.Workspace, BaseURL: llm.URL}
	s, err := session.Open(context.Background(), eng, session.Options{Settings: settings, Uses: []engine.Feature{engine.FeatureRules}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	_, err = s.Submit("tidy up")
	require.NoError(t, err)
	var result core.Result
	for e := range s.Events() {
		if f, ok := e.(core.RunFinished); ok {
			result = f.Result

			break
		}
		if n, ok := e.(session.Notice); ok {
			t.Errorf("no notice for command rules on a gated engine, got %q", n.Message)
		}
	}
	assert.Equal(t, core.StatusOK, result.Status)
	assert.FileExists(t, filepath.Join(env.Workspace, "keep"), "the forbidden command did not run")
	assert.FileExists(t, filepath.Join(env.Workspace, "made"), "the allowed command ran")

	reqs := llm.Requests()
	require.Len(t, reqs, 2)
	require.Len(t, reqs[1].ToolOutputs, 3)
	// The commands run at once, so their results come back in any order.
	outputs := strings.Join(reqs[1].ToolOutputs, "\n")
	assert.Contains(t, outputs, "not run: a rule forbids this command: never delete.")
	assert.Contains(t, outputs, "no user can approve it")
}

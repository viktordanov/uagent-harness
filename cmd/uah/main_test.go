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

	"github.com/viktordanov/uagent-harness/testing/fakellm"
	"github.com/viktordanov/uagent-harness/testing/harnesstest"
)

var uahBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "uah-cli")
	if err != nil {
		panic(err)
	}
	uahBin = filepath.Join(dir, "uah")
	// Every uah the tests start has its own home, so none reads, or copies
	// into, the real ~/.uah; the tests that need another home set one.
	if err := os.Setenv("UAH_HOME", filepath.Join(dir, "home")); err != nil {
		panic(err)
	}
	for _, name := range []string{"UAH_CONFIG", "UAH_STATE_DIR", "UAGENT_CONFIG", "UAGENT_STATE_DIR"} {
		if err := os.Unsetenv(name); err != nil {
			panic(err)
		}
	}
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
	for _, command := range []string{"run", "resume", "sessions", "hooks", "config", "doctor"} {
		assert.Regexp(t, `(?m)^\s+`+command+`\b`, res.stdout)
	}
	assert.Contains(t, res.stdout, "a general-purpose harness for unreal-agent-runner")
}

func TestUnknownFlag(t *testing.T) {
	res := uah(t, "--no-such-flag")
	assert.Equal(t, 2, res.code)
	assert.Empty(t, res.stdout)
	assert.Equal(t, 1, strings.Count(res.stderr, "\n"), res.stderr)
	assert.Contains(t, res.stderr, "no-such-flag")
}

func TestTUINeedsATerminal(t *testing.T) {
	res := uah(t)
	assert.Equal(t, 2, res.code)
	assert.Contains(t, res.stderr, "use uah run")
}

// fakeEnv points uah at the fake runner replaying fixture, in a fresh state dir.
func fakeEnv(t *testing.T, fixture string) (*harnesstest.Env, []string) {
	t.Helper()
	e := harnesstest.NewEnv(t)

	return e, []string{
		"UAGENT_RUNNER=" + harnesstest.FakeRunner(t),
		"UAH_ENGINE=process",
		"UAH_STATE_DIR=" + e.StateDir,
		"CODEX_HOME=" + e.CodexHome,
		"FAKERUNNER_FIXTURE=" + fixtures.Path(fixture),
		"FAKERUNNER_ECHO=1",
		"UNREAL_HARNESS_LLM_PROVIDER=",
		"UNREAL_HARNESS_LLM_MODEL=",
		"UAH_HOME=" + filepath.Join(e.StateDir, "..", "home"),
		"FAKERUNNER_CAPTURE=" + e.Capture,
	}
}

func TestInstructionsAndConfig(t *testing.T) {
	e, env := fakeEnv(t, "simple.jsonl")
	configDir := filepath.Join(e.StateDir, "..", "home")
	require.NoError(t, os.MkdirAll(configDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("effort = \"low\"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "AGENTS.md"), []byte("Always answer in haiku."), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(e.Workspace, "AGENTS.md"), []byte("Use tabs in Go files."), 0o600))

	res := uahWith(t, env, "", "run", "--stream", "-C", e.Workspace, "hi")

	require.Equal(t, 0, res.code, res.stderr)
	assert.Contains(t, res.stdout, `"type":"instructions_loaded"`)
	stdin, err := os.ReadFile(filepath.Join(e.Capture, "stdin.json"))
	require.NoError(t, err)
	var req struct {
		SystemPrompt  string `json:"system_prompt"`
		ThinkingLevel string `json:"thinking_level"`
	}
	require.NoError(t, json.Unmarshal(stdin, &req))
	assert.Equal(t, "low", req.ThinkingLevel, "the config file sets the default effort")
	assert.True(t, strings.HasPrefix(req.SystemPrompt, "You are an AI agent running inside an isolated sandbox container."),
		"the runner's own host prompt is kept")
	assert.Less(t, strings.Index(req.SystemPrompt, "haiku"), strings.Index(req.SystemPrompt, "Use tabs"), "user file first, then the workspace")

	t.Run("flags win over the config file, and instructions can be turned off", func(t *testing.T) {
		res := uahWith(t, env, "", "run", "-q", "-e", "max", "--no-instructions", "-C", e.Workspace, "hi")

		require.Equal(t, 0, res.code, res.stderr)
		stdin, err := os.ReadFile(filepath.Join(e.Capture, "stdin.json"))
		require.NoError(t, err)
		assert.Contains(t, string(stdin), `"thinking_level":"max"`)
		assert.NotContains(t, string(stdin), "system_prompt")
	})

	t.Run("model_instructions_file replaces the host prompt on the process engine", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("model_instructions_file = \"system.md\"\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(configDir, "system.md"), []byte("BASE-PROMPT\n"), 0o600))

		res := uahWith(t, env, "", "run", "-q", "--no-instructions", "-C", e.Workspace, "hi")

		require.Equal(t, 0, res.code, res.stderr)
		stdin, err := os.ReadFile(filepath.Join(e.Capture, "stdin.json"))
		require.NoError(t, err)
		assert.Contains(t, string(stdin), `"system_prompt":"BASE-PROMPT\n"`)
	})

	t.Run("a config typo is a usage error", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("efort = \"low\"\n"), 0o600))

		res := uahWith(t, env, "", "run", "-C", e.Workspace, "hi")

		assert.Equal(t, 2, res.code)
		assert.Contains(t, res.stderr, `unknown key "efort"`)
	})
}

func TestRunAndSessions(t *testing.T) {
	e, env := fakeEnv(t, "simple.jsonl")

	first := uahWith(t, env, "", "run", "-C", e.Workspace, "first question")
	require.Equal(t, 0, first.code, first.stderr)
	assert.Equal(t, "hello\n", first.stdout)
	assert.Contains(t, first.stderr, "› first question")
	assert.Contains(t, first.stderr, "run ok")

	elsewhere := uahWith(t, env, "", "sessions")
	require.Equal(t, 0, elsewhere.code, elsewhere.stderr)
	assert.Empty(t, elsewhere.stdout, "sessions are scoped to the current directory, as in Codex")
	assert.Contains(t, elsewhere.stderr, "--all lists every directory")

	all := uahWith(t, env, "", "sessions", "--all")
	require.Equal(t, 0, all.code, all.stderr)
	assert.Contains(t, all.stdout, "DIRECTORY")
	assert.Contains(t, all.stdout, "first question")

	list := uahWith(t, env, "", "sessions", "-C", e.Workspace)
	require.Equal(t, 0, list.code, list.stderr)
	lines := strings.Split(strings.TrimSpace(list.stdout), "\n")
	require.Len(t, lines, 2, list.stdout)
	id := strings.Fields(lines[1])[0]
	assert.Contains(t, lines[1], "first question")
	assert.Equal(t, "run", strings.Fields(lines[1])[6], "uah run marks its sessions, which the resume picker hides")

	env = append(env, "FAKERUNNER_FIXTURE="+fixtures.Path("parallel.jsonl"))
	more := uahWith(t, env, "second\nthird\n", "run", "-C", e.Workspace, "--last", "--stdin")
	require.Equal(t, 0, more.code, more.stderr)
	assert.Contains(t, more.stdout, "A; B")
	assert.Contains(t, more.stderr, "(resumed)")

	show := uahWith(t, env, "", "sessions", "show", id)
	require.Equal(t, 0, show.code, show.stderr)
	assert.Contains(t, show.stdout, "› first question")
	assert.Contains(t, show.stdout, "✓ hello")
	assert.Contains(t, show.stdout, "› second")
	assert.Contains(t, show.stdout, "✓ A; B")

	noneHere := uahWith(t, env, "", "run", "--last", "hi")
	assert.Equal(t, 2, noneHere.code, "no session in this directory")
	assert.Contains(t, noneHere.stderr, "no session to resume")

	resume := uahWith(t, env, "", "resume", id)
	assert.Equal(t, 2, resume.code)
	assert.Contains(t, resume.stderr, "needs a terminal")

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

// TestRunEmbedded runs `uah run` on the embedded engine against the fake
// model, with --stdin messages and --fast.
func TestRunEmbedded(t *testing.T) {
	e := harnesstest.NewEnv(t)
	llm := fakellm.New(t, fakellm.Reply{Text: "Looking.", Commands: []string{"echo hi"}}, fakellm.Reply{Text: "first answer"}, fakellm.Reply{Text: "second answer"})
	env := []string{
		"UAH_ENGINE=embedded",
		"UAH_STATE_DIR=" + e.StateDir,
		"OPENAI_API_KEY=test-key",
		"UNREAL_HARNESS_LLM_PROVIDER=",
		"UNREAL_HARNESS_LLM_MODEL=",
		"UAH_HOME=" + filepath.Join(e.StateDir, "..", "home"),
	}

	res := uahWith(t, env, "and a follow-up\n", "run", "--stdin", "--fast", "--provider", "openai", "-m", "gpt-test",
		"--base-url", llm.URL, "-C", e.Workspace, "first question")

	require.Equal(t, 0, res.code, res.stderr)
	assert.Equal(t, "first answer\nsecond answer\n", res.stdout)
	reqs := llm.Requests()
	require.Len(t, reqs, 3)
	assert.Equal(t, "priority", reqs[0].ServiceTier)
	assert.Equal(t, []string{"first question", "and a follow-up"}, reqs[2].UserTexts)
}

func TestHooksCommand(t *testing.T) {
	e := harnesstest.NewEnv(t)
	configDir := filepath.Join(e.StateDir, "..", "config")
	ws, err := filepath.EvalSymlinks(e.Workspace)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(configDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(
		"[projects.\""+ws+"\"]\ntrusted = true\n[[hooks.Stop]]\ncommand = \"notify-send done\"\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(ws, ".uah"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(ws, ".uah", "config.toml"), []byte(
		"[[hooks.PreToolUse]]\nmatcher = \"Bash\"\ncommand = \"./check.sh\"\n"), 0o600))
	env := []string{"UAH_HOME=" + configDir}

	res := uahWith(t, env, "", "hooks", "-C", ws)
	require.Equal(t, 0, res.code, res.stderr)
	assert.Regexp(t, `PreToolUse\s+Bash\s+project\s+untrusted\s+./check.sh`, res.stdout)
	assert.Regexp(t, `Stop\s+user\s+runs\s+notify-send done`, res.stdout)

	res = uahWith(t, env, "", "hooks", "trust", "-C", ws)
	require.Equal(t, 0, res.code, res.stderr)
	assert.Contains(t, res.stdout, "trusting PreToolUse hook: ./check.sh")
	res = uahWith(t, env, "", "hooks", "-C", ws)
	assert.Regexp(t, `PreToolUse\s+Bash\s+project\s+runs`, res.stdout)
}

// TestRunEmbeddedCompaction checks that compaction reaches `uah run --stream`
// and a reloaded transcript (`uah sessions show`).
func TestRunEmbeddedCompaction(t *testing.T) {
	e := harnesstest.NewEnv(t)
	llm := fakellm.New(t, fakellm.Reply{Commands: []string{"echo hi"}, InputTokens: 250_000}, fakellm.Reply{Text: "THE SUMMARY"}, fakellm.Reply{Text: "answer"})
	configHome := filepath.Join(e.StateDir, "..", "config")
	require.NoError(t, os.MkdirAll(configHome, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(configHome, "config.toml"), []byte("auto_compact_percent = 90\n"), 0o600))
	env := []string{
		"UAH_ENGINE=embedded", "UAH_STATE_DIR=" + e.StateDir, "OPENAI_API_KEY=test-key",
		"UNREAL_HARNESS_LLM_PROVIDER=", "UNREAL_HARNESS_LLM_MODEL=", "UAH_HOME=" + configHome,
	}

	res := uahWith(t, env, "", "run", "--stream", "--provider", "openai", "-m", "gpt-test", "--base-url", llm.URL, "-C", e.Workspace, "go")
	require.Equal(t, 0, res.code, res.stderr)
	var compacted struct {
		Type    string `json:"type"`
		Trigger string `json:"trigger"`
		Summary string `json:"summary"`
	}
	var types []string
	for line := range strings.Lines(res.stdout) {
		var ev struct {
			Type string `json:"type"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &ev), line)
		types = append(types, ev.Type)
		if ev.Type == "compacted" {
			require.NoError(t, json.Unmarshal([]byte(line), &compacted))
		}
	}
	assert.Contains(t, types, "compaction_started")
	assert.Equal(t, "auto", compacted.Trigger)
	assert.Equal(t, "THE SUMMARY", compacted.Summary)

	show := uahWith(t, env, "", "sessions", "show", "--state-dir", e.StateDir, "--all", lastSessionID(t, e.StateDir))
	require.Equal(t, 0, show.code, show.stderr)
	assert.Contains(t, show.stdout, "⋯ context compacted (auto, 11-char summary)")
}

// lastSessionID is the one session file's ID in stateDir.
func lastSessionID(t *testing.T, stateDir string) string {
	t.Helper()
	logs, err := filepath.Glob(filepath.Join(stateDir, "sessions", "*.compaction.jsonl"))
	require.NoError(t, err)
	require.Len(t, logs, 1)

	return strings.TrimSuffix(filepath.Base(logs[0]), ".compaction.jsonl")
}

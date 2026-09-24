package main_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompletionScripts(t *testing.T) {
	for shell, want := range map[string]string{"bash": "complete", "zsh": "#compdef uah", "fish": "complete -c uah"} {
		res := uah(t, "completion", shell)
		require.Equal(t, 0, res.code, res.stderr)
		assert.Contains(t, res.stdout, want, shell)
	}
}

func TestCompletionValues(t *testing.T) {
	for args, want := range map[string][]string{
		"--effort":           {"low", "medium", "high", "xhigh", "max"},
		"run --sandbox":      {"read-only", "workspace-write", "danger-full-access"},
		"--engine":           {"embedded", "process"},
		"run --ask":          {"on-request", "never"},
		"--provider":         {"openai", "openai-codex", "openrouter", "fireworks", "ollama"},
		"config --log-level": {"debug", "error", "info", "warn"},
	} {
		res := uah(t, append(strings.Fields(args), "--generate-shell-completion")...)
		require.Equal(t, 0, res.code, res.stderr)
		assert.Equal(t, want, strings.Fields(res.stdout), args)
	}
	res := uah(t, "--generate-shell-completion")
	assert.Contains(t, res.stdout, "resume:", "commands complete at the top")
}

func TestCompletionSessionIDs(t *testing.T) {
	e, env := fakeEnv(t, "simple.jsonl")
	res := uahWith(t, env, "", "run", "-C", e.Workspace, "hello")
	require.Equal(t, 0, res.code, res.stderr)

	ids := uahWith(t, env, "", "sessions", "show", "--generate-shell-completion")
	require.Equal(t, 0, ids.code, ids.stderr)
	assert.Len(t, strings.Fields(ids.stdout), 1)
	flag := uahWith(t, env, "", "--session", "--generate-shell-completion")
	assert.Equal(t, ids.stdout, flag.stdout)
}

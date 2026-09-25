package main_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/testing/fixtures"
)

const migratedLine = "uah: moved your config and sessions to ~/.uah (the old folders are untouched)\n"

// oldFolders writes a user file to ~/.config/uagent and one session to
// ~/.local/state/unreal-agent under a fresh HOME, and returns the HOME and
// the environment of a uah that uses the default home there.
func oldFolders(t *testing.T) (string, []string) {
	t.Helper()
	e, env := fakeEnv(t, "simple.jsonl")
	userHome := t.TempDir()
	oldState := filepath.Join(userHome, ".local", "state", "unreal-agent")
	res := uahWith(t, append(env, "UAH_STATE_DIR="+oldState), "", "run", "-C", e.Workspace, "first question")
	require.Equal(t, 0, res.code, res.stderr)
	writeFile(t, filepath.Join(userHome, ".config", "uagent", "config.toml"), "effort = \"low\"\n")

	return userHome, []string{
		"HOME=" + userHome, "UAH_HOME=", // TestMain unsets UAH_CONFIG and UAH_STATE_DIR
		"XDG_CONFIG_HOME=", "XDG_STATE_HOME=", "CODEX_HOME=" + e.CodexHome,
		"FAKERUNNER_FIXTURE=" + fixtures.Path("simple.jsonl"),
	}
}

func TestFirstStartMigrates(t *testing.T) {
	userHome, env := oldFolders(t)

	first := uahWith(t, env, "", "sessions", "--all")
	require.Equal(t, 0, first.code, first.stderr)
	assert.Equal(t, 1, strings.Count(first.stderr, migratedLine), first.stderr)
	assert.Contains(t, first.stdout, "first question")
	assert.FileExists(t, filepath.Join(userHome, ".uah", "config.toml"))

	again := uahWith(t, env, "", "config")
	require.Equal(t, 0, again.code, again.stderr)
	assert.NotContains(t, again.stderr, migratedLine, "the migration runs once")
	assert.Contains(t, again.stdout, "home:         "+filepath.Join(userHome, ".uah")+"\n")
	assert.Contains(t, again.stdout, "user file:    "+filepath.Join(userHome, ".uah", "config.toml")+" (read)")
	assert.Regexp(t, `effort\s+low\s+user file`, again.stdout)
}

func TestCompletionMigratesQuietly(t *testing.T) {
	userHome, env := oldFolders(t)

	res := uahWith(t, env, "", "sessions", "show", "--generate-shell-completion")
	require.Equal(t, 0, res.code, res.stderr)
	assert.Empty(t, res.stderr)
	assert.NotEmpty(t, res.stdout, "the migrated session completes")
	assert.FileExists(t, filepath.Join(userHome, ".uah", "config.toml"))
}

func TestOldVariablesWarn(t *testing.T) {
	root := t.TempDir()
	res := uahWith(t, []string{"UAGENT_CONFIG=" + filepath.Join(root, "config.toml"), "UAGENT_STATE_DIR=" + root}, "", "sessions", "--all")

	require.Equal(t, 0, res.code, res.stderr)
	assert.True(t, strings.HasPrefix(res.stderr, "uah: UAGENT_CONFIG is no longer read; set UAH_CONFIG instead\n"+
		"uah: UAGENT_STATE_DIR is no longer read; set UAH_STATE_DIR instead\n"), res.stderr)
}

func TestOldProjectDirectoryNotice(t *testing.T) {
	e, env := fakeEnv(t, "simple.jsonl")
	require.NoError(t, os.Mkdir(filepath.Join(e.Workspace, ".uagent"), 0o700))

	res := uahWith(t, env, "", "run", "-C", e.Workspace, "hi")

	require.Equal(t, 0, res.code, res.stderr)
	assert.Contains(t, res.stderr, filepath.Join(e.Workspace, ".uagent")+" is no longer read")
	assert.Contains(t, res.stderr, "`mv .uagent .uah`")
	assert.DirExists(t, filepath.Join(e.Workspace, ".uagent"), "uah moves nothing")

	doctor := uahWith(t, env, "", "doctor", "-C", e.Workspace)
	assert.Contains(t, doctor.stdout, ".uagent is no longer read")
}

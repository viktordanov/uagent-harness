package instructions_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/instructions"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func paths(files []instructions.File) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Path)
	}

	return out
}

func TestDiscover(t *testing.T) {
	t.Run("walks from the repository root down to the workspace", func(t *testing.T) {
		root := t.TempDir()
		repo, ws := filepath.Join(root, "repo"), filepath.Join(root, "repo", "svc", "api")
		require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o700))
		require.NoError(t, os.MkdirAll(ws, 0o700))
		write(t, filepath.Join(root, "AGENTS.md"), "outside the repo: ignored")
		write(t, filepath.Join(repo, "AGENTS.md"), "repo rules")
		write(t, filepath.Join(repo, "svc", "CLAUDE.md"), "service rules")
		write(t, filepath.Join(ws, "AGENTS.md"), "api rules")
		write(t, filepath.Join(ws, "AGENTS.override.md"), "api override")
		user := filepath.Join(root, "user", "AGENTS.md")
		write(t, user, "user rules")

		files, err := instructions.Discover(ws, []string{user, filepath.Join(root, "codex", "AGENTS.md")}, instructions.Options{})

		require.NoError(t, err)
		assert.Equal(t, []string{
			user,
			filepath.Join(repo, "AGENTS.md"),
			filepath.Join(ws, "AGENTS.override.md"),
		}, paths(files), "CLAUDE.md is not read by default, as in Codex")

		files, err = instructions.Discover(ws, nil, instructions.Options{FallbackFilenames: []string{"CLAUDE.md", "../x"}})
		require.NoError(t, err)
		assert.Contains(t, paths(files), filepath.Join(repo, "svc", "CLAUDE.md"), "a configured fallback is read")
	})

	t.Run("root markers", func(t *testing.T) {
		root := t.TempDir()
		ws := filepath.Join(root, "proj", "ws")
		write(t, filepath.Join(root, "proj", ".hg"), "")
		write(t, filepath.Join(root, "proj", "AGENTS.md"), "project")
		write(t, filepath.Join(ws, "AGENTS.md"), "workspace")

		files, err := instructions.Discover(ws, nil, instructions.Options{RootMarkers: []string{".hg"}})
		require.NoError(t, err)
		assert.Len(t, files, 2)
		files, err = instructions.Discover(ws, nil, instructions.Options{RootMarkers: []string{}})
		require.NoError(t, err)
		assert.Equal(t, []string{filepath.Join(ws, "AGENTS.md")}, paths(files), "no markers: the workspace only")
	})

	t.Run("outside a repository only the workspace counts", func(t *testing.T) {
		root := t.TempDir()
		ws := filepath.Join(root, "ws")
		write(t, filepath.Join(root, "AGENTS.md"), "parent")
		write(t, filepath.Join(ws, "AGENTS.md"), "workspace")

		files, err := instructions.Discover(ws, nil, instructions.Options{})

		require.NoError(t, err)
		assert.Equal(t, []string{filepath.Join(ws, "AGENTS.md")}, paths(files))
	})

	t.Run("the first existing user file wins and empty files are skipped", func(t *testing.T) {
		root := t.TempDir()
		empty, codex := filepath.Join(root, "a", "AGENTS.md"), filepath.Join(root, "b", "AGENTS.md")
		write(t, empty, "")
		write(t, codex, "codex rules")

		files, err := instructions.Discover(filepath.Join(root, "ws"), []string{empty, codex}, instructions.Options{})

		require.NoError(t, err)
		assert.Equal(t, []string{codex}, paths(files))
	})
}

func TestAssemble(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a.md"), filepath.Join(root, "b.md")
	write(t, a, "first file")
	write(t, b, strings.Repeat("x", 100))
	files := []instructions.File{{Path: a}, {Path: b}}

	t.Run("joins files under headers", func(t *testing.T) {
		text, used, truncated, err := instructions.Assemble(files, 0)

		require.NoError(t, err)
		assert.False(t, truncated)
		assert.Len(t, used, 2)
		assert.Contains(t, text, "## "+a+"\n\nfirst file\n")
		assert.Less(t, strings.Index(text, a), strings.Index(text, b), "order is kept")
	})

	t.Run("stops before the file that passes the cap", func(t *testing.T) {
		text, used, truncated, err := instructions.Assemble(files, 80)

		require.NoError(t, err)
		assert.True(t, truncated)
		assert.Equal(t, []string{a}, paths(used))
		assert.NotContains(t, text, "xxx")
	})

	t.Run("cuts an oversized first file on a character boundary", func(t *testing.T) {
		big := filepath.Join(root, "big.md")
		write(t, big, strings.Repeat("é", 100))

		limit := len("## "+big+"\n\n") + 11 // five and a half two-byte characters

		text, used, truncated, err := instructions.Assemble([]instructions.File{{Path: big}}, limit)

		require.NoError(t, err)
		assert.True(t, truncated)
		assert.Len(t, used, 1)
		assert.LessOrEqual(t, len(text), limit)
		assert.True(t, strings.HasSuffix(text, "é"), "no split character")
	})
}

func TestHostPrompt(t *testing.T) {
	assert.Empty(t, instructions.HostPrompt("", " \n"), "no instructions leave the runner's prompt untouched")
	prompt := instructions.HostPrompt("", "## AGENTS.md\n\nuse tabs\n")
	assert.Equal(t, instructions.RunnerHostPrompt+"\n"+instructions.ProjectHeader+"\n## AGENTS.md\n\nuse tabs\n", prompt,
		"the runner's default text comes first")

	assert.Equal(t, "Be brief.\n", instructions.HostPrompt("Be brief.", ""), "a base prompt alone replaces the runner's")
	prompt = instructions.HostPrompt("Be brief.", "## AGENTS.md\n\nuse tabs\n")
	assert.Equal(t, "Be brief.\n\n"+instructions.ProjectHeader+"\n## AGENTS.md\n\nuse tabs\n", prompt, "instructions follow a base prompt")
	assert.Equal(t, instructions.RunnerHostPrompt, instructions.HostPrompt(strings.TrimSpace(instructions.RunnerHostPrompt), ""),
		"system.md, trimmed as Codex reads it, gives back the runner's text")
}

// TestCodexPrompt pins the embedded copy of Codex's prompt: its opening
// line and its size at rust-v0.156.1.
func TestCodexPrompt(t *testing.T) {
	assert.True(t, strings.HasPrefix(instructions.CodexPrompt, "You are Codex, an agent based on GPT-6."))
	assert.Len(t, instructions.CodexPrompt, 21269)
	assert.NotContains(t, instructions.CodexPrompt, "\n"+instructions.ProjectHeader, "/context can tell it from the instructions")
}

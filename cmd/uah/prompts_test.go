package main_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/contextprep"
	"github.com/viktordanov/uah/internal/instructions"
)

// TestPrompts writes the built-in prompts beside the configuration file,
// uah's default system prompt, Codex's, and the runner's among them,
// refuses to overwrite them without --force, prints the keys that use them
// (the alternatives commented out), and prints one prompt; the written
// keys load.
func TestPrompts(t *testing.T) {
	user, env := mcpEnv(t)
	dir := filepath.Join(filepath.Dir(user), "prompts")

	res := uahWith(t, env, "", "prompts", "init")
	require.Equal(t, 0, res.code, res.stderr)
	compact := filepath.Join(dir, "compact.md")
	system := filepath.Join(dir, "system.md")
	codex := filepath.Join(dir, "system-codex.md")
	runner := filepath.Join(dir, "system-runner.md")
	review := filepath.Join(dir, "review.md")
	defaults := filepath.Join(dir, "context.defaults")
	live := filepath.Join(dir, "context")
	assert.Equal(t, "Wrote the prompts to "+dir+": compact.md, system.md, system-codex.md, system-runner.md, review.md.\n"+
		"A prompt file takes effect only when the configuration names it. To use them, add to "+user+":\n\n"+
		"experimental_compact_prompt_file = \""+compact+"\"\n\n"+
		"model_instructions_file = \""+system+"\"\n"+
		"# Or Codex's own prompt, unmodified (gpt-6.1-sol's; it names Codex's tools, see docs/configuration.md):\n"+
		"# model_instructions_file = \""+codex+"\"\n"+
		"# Or the runner's short host prompt, uah's default before the Codex-based one:\n"+
		"# model_instructions_file = \""+runner+"\"\n\n"+
		"[review]\npolicy_file = \""+review+"\"\n\n"+
		"Wrote "+strconv.Itoa(len(contextprep.Builtins()))+" context modules to "+defaults+" as a reference. uah never reads that folder:\n"+
		"the built-in modules stay in use, and later versions' text reaches you. To change a module,\n"+
		"copy its file to the same path under "+live+" and edit the copy, for example:\n\n"+
		"  mkdir -p "+filepath.Join(live, "environment")+"\n"+
		"  cp "+filepath.Join(defaults, "environment", "fish.md")+" "+filepath.Join(live, "environment", "fish.md")+"\n\n"+
		"Warning: a file under "+live+" replaces the built-in module of the same path for as long as it\n"+
		"exists, so later versions' changes to that module do not reach you. Copy only the modules you change.\n"+
		"Modules of your own go in "+filepath.Join(dir, "context.d")+".\n\n"+
		"Context overrides in use: none.\n\n"+
		"To undo: delete the prompt files and the configuration lines above, and "+defaults+".\n"+
		"`uah prompts status` lists what is in use, `uah prompts prune` deletes overrides identical to the built-in,\n"+
		"and `uah context` shows which modules apply in a workspace.\n", res.stdout)
	assert.NoDirExists(t, live, "init overrides no built-in module")
	fish, err := os.ReadFile(filepath.Join(defaults, "environment", "fish.md"))
	require.NoError(t, err)
	shown := uahWith(t, env, "", "prompts", "show", "context/environment/fish")
	require.Equal(t, 0, shown.code, shown.stderr)
	assert.Equal(t, string(fish), shown.stdout, "a context module, front matter and all")
	assert.True(t, strings.HasPrefix(shown.stdout, "---\nid: fish\n"), shown.stdout)
	_, err = os.Stat(filepath.Join(defaults, "library", "go.md"))
	require.NoError(t, err, "the library too")
	shown = uahWith(t, env, "", "prompts", "show", "context/environment/nope")
	assert.Equal(t, 2, shown.code)
	assert.Contains(t, shown.stderr, "no context module environment/nope")
	data, err := os.ReadFile(review)
	require.NoError(t, err)
	shown = uahWith(t, env, "", "prompts", "show", "review")
	require.Equal(t, 0, shown.code, shown.stderr)
	assert.Equal(t, string(data), shown.stdout)
	assert.Contains(t, shown.stdout, "risk")
	shown = uahWith(t, env, "", "prompts", "show", "compact")
	assert.Contains(t, shown.stdout, "CONTEXT CHECKPOINT COMPACTION")
	for name, want := range map[string]string{
		"system": instructions.DefaultPrompt, "system-codex": instructions.CodexPrompt, "system-runner": instructions.RunnerHostPrompt,
	} {
		data, err := os.ReadFile(filepath.Join(dir, name+".md"))
		require.NoError(t, err)
		assert.Equal(t, want, string(data), "%s.md is the prompt verbatim", name)
		shown = uahWith(t, env, "", "prompts", "show", name)
		require.Equal(t, 0, shown.code, shown.stderr)
		assert.Equal(t, want, shown.stdout)
	}

	require.NoError(t, os.WriteFile(review, []byte("mine\n"), 0o600))
	res = uahWith(t, env, "", "prompts", "init")
	assert.Equal(t, 2, res.code)
	assert.Contains(t, res.stderr, "exists; use --force")
	data, err = os.ReadFile(review)
	require.NoError(t, err)
	assert.Equal(t, "mine\n", string(data), "not overwritten")
	res = uahWith(t, env, "", "prompts", "init", "--force")
	require.Equal(t, 0, res.code, res.stderr)

	f, err := os.OpenFile(user, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString("experimental_compact_prompt_file = \"" + compact + "\"\n\nmodel_instructions_file = \"" + codex + "\"\n\n[review]\npolicy_file = \"" + review + "\"\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	res = uahWith(t, env, "", "config")
	require.Equal(t, 0, res.code, res.stderr)
	assert.Contains(t, res.stdout, review)
	assert.Contains(t, res.stdout, codex, "the alternative prompt loads when the key names it")

	res = uahWith(t, env, "", "prompts", "show", "other")
	assert.Equal(t, 2, res.code)
	assert.Contains(t, res.stderr, "compact, system, system-codex, system-runner, review")
}

// TestPromptsStatusAndPrune: status lists the prompt files, the context
// overrides (a copy identical to the built-in flagged as pinned, an edited
// one in use, a broken one not used), and the user's own modules; prune
// deletes only the identical copies, and the folders that leaves empty.
func TestPromptsStatusAndPrune(t *testing.T) {
	user, env := mcpEnv(t)
	dir := filepath.Join(filepath.Dir(user), "prompts")
	darwin, err := contextprep.BuiltinFile("os/darwin")
	require.NoError(t, err)
	writeFile(t, filepath.Join(dir, "context", "os", "darwin.md"), darwin)
	writeFile(t, filepath.Join(dir, "context", "environment", "fish.md"), "---\nid: fish\ndescription: mine\nwhen: {shell: [fish]}\n---\nMy fish notes.\n")
	writeFile(t, filepath.Join(dir, "context", "plan9.md"), "---\nid: plan9\ndescription: x\n---\nPlan 9.\n")
	writeFile(t, filepath.Join(dir, "context.d", "my-go.md"), "---\nid: my-go\ndescription: x\nenabled: false\n---\nRun mage test.\n")
	writeFile(t, filepath.Join(dir, "system.md"), instructions.DefaultPrompt)

	res := uahWith(t, env, "", "prompts", "status")
	require.Equal(t, 0, res.code, res.stderr)
	assert.Regexp(t, `system\.md +identical to the built-in`, res.stdout)
	assert.Regexp(t, `review\.md +absent`, res.stdout)
	assert.Regexp(t, `os/darwin +pinned copy: identical to the built-in, so no effect today, but it stops later versions' text`, res.stdout)
	assert.Regexp(t, `environment/fish +edited: in use`, res.stdout)
	assert.Regexp(t, `plan9 +not used, the built-in stays: no built-in module plan9`, res.stdout)
	assert.Regexp(t, `my-go +enabled: false, used where \[context\] modules names my-go`, res.stdout)

	res = uahWith(t, env, "", "prompts", "prune", "--dry-run")
	require.Equal(t, 0, res.code, res.stderr)
	assert.Equal(t, "would delete "+filepath.Join(dir, "context", "os", "darwin.md")+"\n"+
		"1 override(s) identical to the built-in would be deleted; 2 kept.\n", res.stdout)
	assert.FileExists(t, filepath.Join(dir, "context", "os", "darwin.md"))

	res = uahWith(t, env, "", "prompts", "prune")
	require.Equal(t, 0, res.code, res.stderr)
	assert.Contains(t, res.stdout, "deleted "+filepath.Join(dir, "context", "os", "darwin.md")+"\n")
	assert.NoDirExists(t, filepath.Join(dir, "context", "os"), "the folder it left empty")
	assert.FileExists(t, filepath.Join(dir, "context", "environment", "fish.md"))
	assert.FileExists(t, filepath.Join(dir, "context", "plan9.md"))
	res = uahWith(t, env, "", "prompts", "prune")
	assert.Equal(t, "No context override is identical to the built-in; 2 kept.\n", res.stdout)

	res = uahWith(t, env, "", "prompts", "init", "--force")
	require.Equal(t, 0, res.code, res.stderr)
	assert.Contains(t, res.stdout, "Context overrides in use: 1; 1 more not used because of an error (`uah prompts status` says which).\n")
}

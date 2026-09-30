package bubble

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/approval"
	"github.com/viktordanov/uah/internal/sandbox"
)

// TestEditorCommand: $VISUAL wins over $EDITOR, the command splits into
// words as a shell would, and without either the editor is vim (vi when
// vim is missing).
func TestEditorCommand(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(key string) string { return vars[key] }
	}
	cases := []struct {
		name string
		vars map[string]string
		want []string
	}{
		{"visual first", map[string]string{"VISUAL": "nvim -f", "EDITOR": "nano"}, []string{"nvim", "-f"}},
		{"editor", map[string]string{"EDITOR": "code --wait"}, []string{"code", "--wait"}},
		{"blank visual", map[string]string{"VISUAL": "  ", "EDITOR": "hx"}, []string{"hx"}},
		{"quotes", map[string]string{"VISUAL": `"/Applications/My Editor/bin/edit" --new-window 'a b'`}, []string{"/Applications/My Editor/bin/edit", "--new-window", "a b"}},
		{"escapes", map[string]string{"EDITOR": `/opt/my\ editor -w`}, []string{"/opt/my editor", "-w"}},
		{"variables", map[string]string{"EDITOR": "$HOME/bin/ed -x", "HOME": "/home/u"}, []string{"/home/u/bin/ed", "-x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := editorCommand(env(tc.vars))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	got, err := editorCommand(env(nil))
	require.NoError(t, err)
	want := "vi"
	if _, err := exec.LookPath("vim"); err == nil {
		want = "vim"
	}
	assert.Equal(t, []string{want}, got)

	_, err = editorCommand(env(map[string]string{"EDITOR": `vim "unclosed`}))
	require.Error(t, err)
	_, err = editorCommand(env(map[string]string{"EDITOR": `""`}))
	require.ErrorIs(t, err, errNoEditor)
}

// TestDraftDirExposed: uah's home is out of the sandbox's reach in every
// permission mode, also with the user's home as the workspace, since .uah
// stays protected in a writable root; a home the sandbox writes under
// another name is refused, except in yolo mode, which has no sandbox.
func TestDraftDirExposed(t *testing.T) {
	t.Setenv("TMPDIR", "") // the sandbox's writable roots are the ones below and /tmp
	userHome := "/uah-test/home"
	project := "/uah-test/project"
	draftDir := filepath.Join(userHome, ".uah", "editor")
	policy := func(mode approval.Mode, workspace string, roots ...string) sandbox.Policy {
		return sandbox.Policy{Mode: mode.Sandbox(), Workspace: workspace, WritableRoots: roots}
	}
	for _, mode := range approval.Modes {
		t.Run(string(mode), func(t *testing.T) {
			assert.False(t, draftDirExposed(policy(mode, project), draftDir))
			assert.False(t, draftDirExposed(policy(mode, userHome), draftDir), "the workspace is the user's home")
			assert.False(t, draftDirExposed(policy(mode, project), filepath.Join(project, ".uah", "editor")), "$UAH_HOME is the project's .uah")

			exposed := mode.Sandbox() == sandbox.WorkspaceWrite
			assert.Equal(t, exposed, draftDirExposed(policy(mode, project), filepath.Join(project, "uah-home", "editor")), "$UAH_HOME in the workspace")
			assert.Equal(t, exposed, draftDirExposed(policy(mode, project), "/tmp/uah-home/editor"), "$UAH_HOME in /tmp")
			assert.Equal(t, exposed, draftDirExposed(policy(mode, project, filepath.Join(userHome, ".uah")), draftDir), "a writable root is uah's home")
			assert.Equal(t, exposed, draftDirExposed(policy(mode, project, filepath.Join(draftDir, "sub")), draftDir), "a writable root inside the directory")
		})
	}
}

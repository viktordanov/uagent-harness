package bubble

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

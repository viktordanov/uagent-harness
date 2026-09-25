package clipboard_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/images/clipboard"
)

// fakeInput records the commands a writer runs and what it gave them.
type fakeInput struct {
	calls []string
	fail  bool
}

func (f *fakeInput) run(_ context.Context, stdin, name string, args ...string) error {
	f.calls = append(f.calls, strings.TrimSpace(name+" "+strings.Join(args, " "))+" <- "+stdin)
	if f.fail {
		return errors.New("exit status 1")
	}

	return nil
}

func writer(f *fakeInput, goos string, tools []string, env map[string]string) clipboard.TextWriter {
	return clipboard.TextWriter{
		Run: f.run, GOOS: goos, Getenv: func(k string) string { return env[k] },
		LookPath: func(name string) (string, error) {
			for _, t := range tools {
				if t == name {
					return "/usr/bin/" + name, nil
				}
			}

			return "", errors.New("not found")
		},
	}
}

func TestWriteText_PicksTheSystemTool(t *testing.T) {
	cases := []struct {
		name, goos string
		tools      []string
		env        map[string]string
		want       string
	}{
		{"macOS", "darwin", []string{"pbcopy"}, nil, "pbcopy <- hi"},
		{"Wayland", "linux", []string{"wl-copy", "xclip"}, map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}, "wl-copy <- hi"},
		{"X11", "linux", []string{"wl-copy", "xclip"}, map[string]string{"DISPLAY": ":0"}, "xclip -selection clipboard <- hi"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeInput{}
			require.NoError(t, writer(f, c.goos, c.tools, c.env).WriteText(t.Context(), "hi"))
			assert.Equal(t, []string{c.want}, f.calls)
		})
	}
}

func TestWriteText_NoTool(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		f := &fakeInput{}
		err := writer(f, goos, nil, map[string]string{"DISPLAY": ":0"}).WriteText(t.Context(), "hi")
		require.ErrorIs(t, err, clipboard.ErrNoTextTool, goos)
		assert.Empty(t, f.calls)
	}
}

func TestWriteText_ToolFails(t *testing.T) {
	f := &fakeInput{fail: true}
	err := writer(f, "darwin", []string{"pbcopy"}, nil).WriteText(t.Context(), "hi")
	require.ErrorContains(t, err, "failed to copy with pbcopy")
}

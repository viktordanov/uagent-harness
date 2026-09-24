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

// fakeExec answers commands by their joined arguments; anything else fails,
// as a clipboard tool fails for a type the clipboard lacks.
type fakeExec struct {
	out   map[string]string
	calls []string
}

func (f *fakeExec) run(_ context.Context, name string, args ...string) ([]byte, error) {
	cmd := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, cmd)
	if out, ok := f.out[cmd]; ok {
		return []byte(out), nil
	}

	return nil, errors.New("exit status 1")
}

func TestMacOS_PNG(t *testing.T) {
	f := &fakeExec{out: map[string]string{
		"osascript -e the clipboard as «class PNGf»": "«data PNGf89504E470D0A1A0A»\n",
	}}
	got, err := clipboard.MacOS{Exec: f.run}.ReadImage(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, got.Data)
	assert.Empty(t, got.Path)
}

func TestMacOS_FinderFileFirst(t *testing.T) {
	f := &fakeExec{out: map[string]string{
		"osascript -e POSIX path of (the clipboard as «class furl»)": "/Users/me/shot.png\n",
		"osascript -e the clipboard as «class PNGf»":                 "«data PNGf00»",
	}}
	got, err := clipboard.MacOS{Exec: f.run}.ReadImage(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "/Users/me/shot.png", got.Path)
}

func TestMacOS_TIFFAndEmpty(t *testing.T) {
	f := &fakeExec{out: map[string]string{"osascript -e the clipboard as «class TIFF»": "«data TIFF4D4D»"}}
	got, err := clipboard.MacOS{Exec: f.run}.ReadImage(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []byte("MM"), got.Data)

	_, err = clipboard.MacOS{Exec: (&fakeExec{}).run}.ReadImage(t.Context())
	require.ErrorIs(t, err, clipboard.ErrNoImage)
}

func linux(f *fakeExec, env map[string]string, installed ...string) clipboard.Linux {
	return clipboard.Linux{
		Exec: f.run,
		LookPath: func(name string) (string, error) {
			for _, n := range installed {
				if n == name {
					return "/usr/bin/" + name, nil
				}
			}

			return "", errors.New("not found")
		},
		Getenv: func(k string) string { return env[k] },
	}
}

func TestLinux_WaylandPNG(t *testing.T) {
	f := &fakeExec{out: map[string]string{
		"wl-paste --list-types":                     "text/plain\nimage/png\n",
		"wl-paste --no-newline --type image/png": "PNGDATA",
	}}
	got, err := linux(f, map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}, "wl-paste", "xclip").ReadImage(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "PNGDATA", string(got.Data))
}

func TestLinux_X11FileList(t *testing.T) {
	f := &fakeExec{out: map[string]string{
		"xclip -selection clipboard -t TARGETS -o":       "TARGETS\ntext/uri-list\n",
		"xclip -selection clipboard -t text/uri-list -o": "# copied\nfile:///home/me/a%20b.png\n",
	}}
	got, err := linux(f, map[string]string{"DISPLAY": ":0"}, "xclip").ReadImage(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "/home/me/a b.png", got.Path)
}

func TestLinux_NoToolOrNoImage(t *testing.T) {
	_, err := linux(&fakeExec{}, map[string]string{"WAYLAND_DISPLAY": "w"}).ReadImage(t.Context())
	require.ErrorIs(t, err, clipboard.ErrNoTool)

	f := &fakeExec{out: map[string]string{"xclip -selection clipboard -t TARGETS -o": "UTF8_STRING\n"}}
	_, err = linux(f, map[string]string{"DISPLAY": ":0"}, "xclip").ReadImage(t.Context())
	require.ErrorIs(t, err, clipboard.ErrNoImage)
}

package clipboard

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/viktordanov/uagent-harness/internal/images"
)

// imageTypes are the clipboard types read as an image, in order of
// preference.
var imageTypes = []string{"image/png", "image/jpeg", "image/webp", "image/gif", "image/bmp", "image/tiff"}

// uriList is the type file managers copy files as.
const uriList = "text/uri-list"

// ErrNoTool means neither wl-paste nor xclip can be run.
var ErrNoTool = errors.New("pasting an image needs wl-paste (Wayland, from wl-clipboard) or xclip (X11); install one, or paste or drop the image file's path instead")

// Linux reads the clipboard with wl-paste on Wayland and xclip on X11,
// whichever the session has and can run.
type Linux struct {
	Exec     Exec
	LookPath func(string) (string, error)
	Getenv   func(string) string
}

// tool is one clipboard command: how to list the types and read one.
type tool struct {
	list func() []string
	read func(typ string) []string
	name string
}

var (
	wlPaste = tool{
		name: "wl-paste",
		list: func() []string { return []string{"--list-types"} },
		read: func(typ string) []string { return []string{"--no-newline", "--type", typ} },
	}
	xclip = tool{
		name: "xclip",
		list: func() []string { return []string{"-selection", "clipboard", "-t", "TARGETS", "-o"} },
		read: func(typ string) []string { return []string{"-selection", "clipboard", "-t", typ, "-o"} },
	}
)

func (l Linux) ReadImage(ctx context.Context) (Content, error) {
	t, ok := l.pick()
	if !ok {
		return Content{}, ErrNoTool
	}
	out, err := l.Exec(ctx, t.name, t.list()...)
	if err != nil {
		return Content{}, ErrNoImage // an empty clipboard makes both tools fail
	}
	types := strings.Fields(string(out))
	for _, typ := range imageTypes {
		if !slices.Contains(types, typ) {
			continue
		}
		data, err := l.Exec(ctx, t.name, t.read(typ)...)
		if err != nil {
			return Content{}, fmt.Errorf("%s failed to read the clipboard: %w", t.name, err)
		}

		return Content{Data: data}, nil
	}
	if slices.Contains(types, uriList) {
		if list, err := l.Exec(ctx, t.name, t.read(uriList)...); err == nil {
			if path, ok := firstFile(string(list)); ok {
				return Content{Path: path}, nil
			}
		}
	}

	return Content{}, ErrNoImage
}

// pick chooses wl-paste in a Wayland session and xclip in an X11 one, when
// installed.
func (l Linux) pick() (tool, bool) {
	has := func(name string) bool { _, err := l.LookPath(name); return err == nil }
	if l.Getenv("WAYLAND_DISPLAY") != "" && has(wlPaste.name) {
		return wlPaste, true
	}
	if l.Getenv("DISPLAY") != "" && has(xclip.name) {
		return xclip, true
	}

	return tool{}, false
}

// firstFile is the first local image file of a URI list.
func firstFile(list string) (string, bool) {
	for line := range strings.SplitSeq(list, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		u, err := url.Parse(line)
		if err != nil || u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") || !images.IsImagePath(u.Path) {
			continue
		}

		return u.Path, true
	}

	return "", false
}

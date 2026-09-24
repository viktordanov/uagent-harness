package images

import (
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// extensions are the image files a pasted or dropped path attaches.
var extensions = []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff"}

// IsImagePath reports whether a path names an image by its extension.
func IsImagePath(path string) bool {
	return slices.Contains(extensions, strings.ToLower(filepath.Ext(path)))
}

// PastedPath reports whether pasted text is the path of one image file, as a
// terminal pastes a file dropped on it: quoted, shell-escaped, or a file://
// URL. It returns the absolute path of a file that exists. home is the
// user's home directory for "~/" ("" leaves it).
func PastedPath(text, home string) (string, bool) {
	p := strings.TrimSpace(text)
	if p == "" || strings.ContainsAny(p, "\n\r") {
		return "", false
	}
	p, ok := unquote(p)
	if !ok {
		return "", false
	}
	if strings.HasPrefix(p, "file://") {
		u, err := url.Parse(p)
		if err != nil || (u.Host != "" && u.Host != "localhost") {
			return "", false
		}
		p = u.Path
	}
	if home != "" && strings.HasPrefix(p, "~/") {
		p = filepath.Join(home, p[2:])
	}
	if !filepath.IsAbs(p) || !IsImagePath(p) {
		return "", false
	}
	if info, err := os.Stat(p); err != nil || !info.Mode().IsRegular() {
		return "", false
	}

	return filepath.Clean(p), true
}

// unquote removes one pair of surrounding quotes, or else the shell's
// backslash escapes. ok is false when the text is more than one word.
func unquote(p string) (string, bool) {
	if len(p) >= 2 && (p[0] == '"' || p[0] == '\'') && p[len(p)-1] == p[0] {
		return p[1 : len(p)-1], true
	}
	var b strings.Builder
	escaped := false
	for _, r := range p {
		switch {
		case escaped:
			b.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == ' ' || r == '\t':
			return "", false // two words, not one path
		default:
			b.WriteRune(r)
		}
	}

	return b.String(), true
}

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
)

// gitdirTarget returns the directory a ".git" file points to, or "" when
// path is a directory, missing, or not a gitdir file.
func gitdirTarget(path string) string {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	dir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return ""
	}
	dir = strings.TrimSpace(dir)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(filepath.Dir(path), dir)
	}

	return filepath.Clean(dir)
}

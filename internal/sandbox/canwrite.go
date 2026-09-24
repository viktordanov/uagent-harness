package sandbox

import (
	"os"
	"path/filepath"
	"strings"
)

// CanWrite reports whether the policy lets a sandboxed command write path,
// as Codex's can_write_path does for apply_patch: anything in FullAccess,
// nothing in ReadOnly, and in WorkspaceWrite a path under a writable root
// that is not a protected path. Symlinks in the path's existing part are
// resolved first, so a link cannot lead out of a root.
func (p Policy) CanWrite(path string) bool {
	switch p.Mode {
	case FullAccess:
		return true
	case ReadOnly:
		return false
	case WorkspaceWrite:
	}
	path = resolvePath(path)
	roots := p.Writable()
	inside := false
	for _, r := range roots {
		if within(path, r) {
			inside = true
		}
		for _, protected := range Protected(r) {
			if within(path, resolvePath(protected)) {
				return false
			}
		}
	}

	return inside
}

// resolvePath makes path absolute and resolves the symlinks of its deepest
// existing ancestor; a dangling link is followed to its target.
func resolvePath(path string) string {
	return resolveDepth(path, 0)
}

func resolveDepth(path string, depth int) string {
	path, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	rest := ""
	for dir := path; ; {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(resolved, rest)
		}
		if target, err := os.Readlink(dir); err == nil && depth < maxLinks {
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(dir), target)
			}

			return resolveDepth(filepath.Join(target, rest), depth+1)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return path
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
}

// maxLinks bounds how many dangling links resolvePath follows.
const maxLinks = 40

// within reports whether path is root or under it.
func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)

	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

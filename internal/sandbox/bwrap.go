// Adapted from openai/codex rust-v0.156.1 (Apache-2.0):
// codex-rs/linux-sandbox/src/bwrap.rs, create_bwrap_flags and
// create_filesystem_args.

package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// BwrapArgs returns the bubblewrap arguments that come before "--" for the
// policy. The layout follows Codex:
//
//	--new-session --die-with-parent
//	--ro-bind / / --dev /dev                   the whole disk read-only, a minimal /dev
//	--bind R R                                 each existing writable root, shallowest first
//	  --ro-bind P P                            each existing protected path in R
//	  --perms 555 --tmpfs P --remount-ro P     each missing protected name in R
//	--ro-bind G G                              a worktree's gitdir inside a writable root, after every bind
//	--unshare-user --unshare-pid --unshare-ipc
//	--unshare-net                              unless the policy has network
//	--proc /proc --cap-drop ALL
//
// ReadOnly, and FullAccess if asked, have no writable roots. bwrap cannot
// mount on a missing path, so, as Codex does, a missing protected name such as
// .git gets an empty read-only tmpfs: the command cannot create it. bwrap
// creates that empty directory on the host as the mount point;
// BwrapMountTargets lists these so the caller can remove them afterwards.
func BwrapArgs(p Policy) []string {
	args, _ := bwrapLayout(p, true)

	return args
}

// BwrapMountTargets returns the missing protected directories that bwrap
// creates on the host as mount points for BwrapArgs(p), so it must be called
// before the command runs. Codex removes them after the command exits; a
// caller that does the same should only remove them while they are still
// empty, as os.Remove does. Left in place, an empty directory protects as
// well: the next run binds it read-only.
func BwrapMountTargets(p Policy) []string {
	_, targets := bwrapLayout(p, true)

	return targets
}

// bwrapLayout builds BwrapArgs. Without mountProc it leaves out "--proc
// /proc", Codex's fallback for containers that forbid mounting procfs.
func bwrapLayout(p Policy, mountProc bool) (args, targets []string) {
	args = []string{
		"--new-session",
		"--die-with-parent",
		"--ro-bind", "/", "/",
		"--dev", "/dev",
	}
	var roots []string
	for _, r := range p.Writable() {
		// bwrap needs every bind source to exist; Codex skips missing roots.
		if exists(r) {
			roots = append(roots, r)
		}
	}
	slices.SortStableFunc(roots, byDepth)
	// A protected path outside its own root, such as a worktree's gitdir,
	// goes after every bind so a later root bind cannot cover it.
	var later []string
	for _, root := range roots {
		args = append(args, "--bind", root, root)
		protected := Protected(root)
		slices.SortStableFunc(protected, byDepth)
		for _, path := range protected {
			if under(path, []string{root}) {
				args, targets = protect(args, targets, path)
			} else if under(path, roots) && !slices.Contains(later, path) {
				// Outside every root it is already read-only.
				later = append(later, path)
			}
		}
	}
	slices.SortStableFunc(later, byDepth)
	for _, path := range later {
		args, targets = protect(args, targets, path)
	}
	args = append(args, "--unshare-user", "--unshare-pid", "--unshare-ipc")
	if !p.Network {
		args = append(args, "--unshare-net")
	}
	if mountProc {
		args = append(args, "--proc", "/proc")
	}
	args = append(args, "--cap-drop", "ALL")

	return args, targets
}

// protect makes path read-only inside a writable bind: an existing path is
// bound read-only over itself, and a missing protected name gets an empty
// read-only tmpfs. A missing gitdir target is left alone: Codex binds an
// empty file there through an inherited fd, which argv cannot carry.
func protect(args, targets []string, path string) (newArgs, newTargets []string) {
	switch {
	case exists(path):
		args = append(args, "--ro-bind", path, path)
	case slices.Contains(ProtectedNames, filepath.Base(path)):
		args = append(args, "--perms", "555", "--tmpfs", path, "--remount-ro", path)
		targets = append(targets, path)
	}

	return args, targets
}

func exists(path string) bool {
	_, err := os.Lstat(path)

	return err == nil
}

// under reports whether path is inside one of roots.
func under(path string, roots []string) bool {
	for _, r := range roots {
		if rel, err := filepath.Rel(r, path); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return true
		}
	}

	return false
}

func byDepth(a, b string) int {
	return strings.Count(filepath.Clean(a), "/") - strings.Count(filepath.Clean(b), "/")
}

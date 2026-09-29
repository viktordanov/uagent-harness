// Package gitdiff reads a workspace's changes with git for the TUI's /diff
// and /review: the working tree against HEAD (staged and unstaged) plus the
// untracked files as additions, the local branches, the recent commits, and
// a branch's merge base. It runs git read-only and parses its output into
// internal/patch's display diff, which the TUI already draws.
package gitdiff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ErrNotRepo means the directory is not inside a git work tree.
var ErrNotRepo = errors.New("not inside a git repository")

// safeConfig keeps a read from running the repository's own helpers or
// writing to it: no filesystem monitor, and paths as they are. The diff
// commands add --no-ext-diff and --no-textconv, as Codex's /diff does.
var safeConfig = []string{"-c", "core.fsmonitor=false", "-c", "core.quotePath=false"}

// git runs git in dir and returns its standard output.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append(safeConfig, args...)...)
	cmd.Dir = dir
	// No index refresh, so a read never takes the index lock.
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}

		return "", fmt.Errorf("failed to run git %s: %s", args[0], msg)
	}

	return out.String(), nil
}

// Root is the top of the work tree that holds dir, or ErrNotRepo.
func Root(ctx context.Context, dir string) (string, error) {
	out, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("failed to find the repository: %w", ctx.Err())
		}

		return "", ErrNotRepo
	}

	return strings.TrimSpace(out), nil
}

// hasHead reports whether the repository has a commit yet.
func hasHead(ctx context.Context, root string) bool {
	_, err := git(ctx, root, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")

	return err == nil
}

// emptyTree is the ID of the empty tree in the repository's hash, the base
// of a diff before the first commit.
func emptyTree(ctx context.Context, root string) (string, error) {
	out, err := git(ctx, root, "hash-object", "-t", "tree", os.DevNull)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(out), nil
}

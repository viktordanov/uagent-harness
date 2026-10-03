package contextprep

import (
	"cmp"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Workspace's limits: each git command's time, the status lines, and the
// listing's entries.
const (
	gitWait        = 2 * time.Second
	statusLines    = 20
	listingEntries = 40
)

// Workspace is the "workspace" block: git's branch and short status, and
// the files git tracks by top directory, so the first turns need not
// explore. It is empty outside a git repository.
type Workspace struct{}

// Name is the block's name.
func (Workspace) Name() string { return "workspace" }

// Prepare gathers the block with read-only git commands.
func (Workspace) Prepare(ctx context.Context, f Facts) string {
	st := gitState(ctx, f.Workspace)
	if st == "" {
		return ""
	}
	parts := []string{st}
	if files := listing(ctx, f.Workspace, MaxAdapterBytes-len(st)-100); files != "" {
		parts = append(parts, files)
	}

	return strings.Join(parts, "\n\n")
}

// gitState is the branch and the short status, or "" outside a git
// repository.
func gitState(ctx context.Context, workspace string) string {
	branch, err := git(ctx, workspace, "branch", "--show-current")
	if err != nil {
		return ""
	}
	status, err := git(ctx, workspace, "status", "--short")
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(status, "\n"), "\n")
	switch {
	case strings.TrimSpace(status) == "":
		lines = []string{"(clean)"}
	case len(lines) > statusLines:
		lines = append(lines[:statusLines], fmt.Sprintf("(%d more)", len(lines)-statusLines))
	}

	return "Git branch: " + cmp.Or(strings.TrimSpace(branch), "(detached)") + "\ngit status --short:\n" + strings.Join(lines, "\n")
}

// listing summarizes the files git tracks: each top directory with its
// file count, and the files at the top, in at most limit bytes.
func listing(ctx context.Context, workspace string, limit int) string {
	out, err := git(ctx, workspace, "ls-files")
	if err != nil {
		return ""
	}
	files := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if files[0] == "" {
		return ""
	}
	counts := map[string]int{}
	var order []string
	for _, f := range files {
		top, _, nested := strings.Cut(f, "/")
		if nested {
			top += "/"
		}
		if counts[top] == 0 {
			order = append(order, top)
		}
		counts[top]++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Files (git ls-files: %d in all):", len(files))
	for i, top := range order {
		line := "\n" + top
		if strings.HasSuffix(top, "/") {
			line = fmt.Sprintf("\n%s (%d files)", top, counts[top])
		}
		if i == listingEntries || b.Len()+len(line) > limit {
			fmt.Fprintf(&b, "\n(%d more entries)", len(order)-i)

			break
		}
		b.WriteString(line)
	}

	return b.String()
}

// git runs a read-only git command in the workspace.
func git(ctx context.Context, workspace string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", workspace, "--no-optional-locks"}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to run git %s: %w", args[0], err)
	}

	return string(out), nil
}

package gitdiff

import (
	"context"
	"slices"
	"strconv"
	"strings"
)

// Branches are the local branches, as Codex's /review lists them: sorted,
// with the default branch first, and the checked-out branch ("" when HEAD
// is detached).
type Branches struct {
	Names   []string
	Current string
}

// Commit is one recent commit: its full ID and its subject.
type Commit struct {
	SHA     string
	Subject string
}

// RecentCommits is how many commits /review offers, as Codex's picker.
const RecentCommits = 100

// ListBranches reads the local branches of the repository that holds dir.
func ListBranches(ctx context.Context, dir string) (Branches, error) {
	if _, err := Root(ctx, dir); err != nil {
		return Branches{}, err
	}
	out, err := git(ctx, dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return Branches{}, err
	}
	names := strings.Fields(out)
	slices.Sort(names)
	if def := defaultBranch(ctx, dir, names); def != "" {
		i := slices.Index(names, def)
		names = append([]string{def}, slices.Delete(names, i, i+1)...)
	}
	current, _ := git(ctx, dir, "branch", "--show-current")

	return Branches{Names: names, Current: strings.TrimSpace(current)}, nil
}

// defaultBranch is origin's HEAD when it names a local branch, else main
// or master when one exists.
func defaultBranch(ctx context.Context, dir string, names []string) string {
	if out, err := git(ctx, dir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if name := strings.TrimPrefix(strings.TrimSpace(out), "origin/"); slices.Contains(names, name) {
			return name
		}
	}
	for _, name := range []string{"main", "master"} {
		if slices.Contains(names, name) {
			return name
		}
	}

	return ""
}

// ListCommits reads the newest n commits of HEAD, newest first; a
// repository without commits has none.
func ListCommits(ctx context.Context, dir string, n int) ([]Commit, error) {
	root, err := Root(ctx, dir)
	if err != nil {
		return nil, err
	}
	if !hasHead(ctx, root) {
		return nil, nil
	}
	out, err := git(ctx, root, "log", "-n", strconv.Itoa(n), "--pretty=format:%H%x1f%s")
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for line := range strings.SplitSeq(out, "\n") {
		if sha, subject, ok := strings.Cut(line, "\x1f"); ok {
			commits = append(commits, Commit{SHA: sha, Subject: subject})
		}
	}

	return commits, nil
}

// MergeBase is the commit HEAD and branch share, the base Codex's /review
// diffs against: against the branch's upstream when the upstream is ahead
// of the local branch, as Codex's merge_base_with_head does. It returns ""
// without an error when there is none (no commits yet, an unknown branch).
func MergeBase(ctx context.Context, dir, branch string) (string, error) {
	root, err := Root(ctx, dir)
	if err != nil {
		return "", err
	}
	if !hasHead(ctx, root) {
		return "", nil
	}
	ref := branch
	if up, err := git(ctx, root, "rev-parse", "--abbrev-ref", "--symbolic-full-name", branch+"@{upstream}"); err == nil {
		up = strings.TrimSpace(up)
		if _, err := git(ctx, root, "merge-base", "--is-ancestor", branch, up); err == nil {
			ref = up // the upstream has everything the local branch has, and maybe more
		}
	}
	out, err := git(ctx, root, "merge-base", "HEAD", ref)
	if err != nil {
		return "", nil // no merge base: the prompt tells the reviewer to find it
	}

	return strings.TrimSpace(out), nil
}

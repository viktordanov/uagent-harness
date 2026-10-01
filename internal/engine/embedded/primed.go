package embedded

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/session"
)

// The primed-first-turn experiment: a new session of the main agent starts
// with one more user message before the user's, a compact workspace
// context that uah gathers cheaply, so the model's first turns need not
// explore or read what AGENTS.md includes: the files git tracks, by top
// directory, git's branch and status, and the files AGENTS.md includes
// with an @ line (such as @RTK.md), which uah does not expand in the
// system prompt. The system prompt stays the same, byte for byte, so the
// prompt cache holds. Subagents, forks, and resumed sessions get none.

// Primed context's limits: the whole block, the git commands, the status
// lines, and the listing's entries.
const (
	primedMax      = 4 << 10
	primedGitWait  = 2 * time.Second
	primedStatus   = 20
	primedListing  = 40
	primedIncludes = 3 << 10
)

// primed adds the workspace context before the messages of a new main
// session, when the experiment is on.
func (w *wiring) primed(ctx context.Context, req core.Request, messages []core.UserInput) []core.UserInput {
	if !w.e.experiments.primedFirstTurn || strings.HasPrefix(req.SessionID, session.SubagentIDPrefix) {
		return messages
	}
	if req.SessionID != "" {
		_, err := os.Stat(filepath.Join(w.l.SessionsDir, req.SessionID+".session.jsonl"))
		if !errors.Is(err, fs.ErrNotExist) {
			return messages // resumed or forked
		}
	}
	text := primedContext(ctx, req.Workspace, req.SystemPrompt)
	if text == "" {
		return messages
	}

	return append([]core.UserInput{{ID: uuid.NewString(), Text: text}}, messages...)
}

// primedContext is the workspace context block, or "" when there is
// nothing to say.
func primedContext(ctx context.Context, workspace, systemPrompt string) string {
	var parts []string
	if inc := includes(systemPrompt); inc != "" {
		parts = append(parts, inc)
	}
	if st := gitState(ctx, workspace); st != "" {
		parts = append(parts, st)
	}
	used := 0
	for _, p := range parts {
		used += len(p) + 2
	}
	if files := listing(ctx, workspace, primedMax-used-200); files != "" {
		parts = append(parts, files)
	}
	if len(parts) == 0 {
		return ""
	}
	text := "<workspace_context>\nuah gathered this at the start of the session, so you need not look it up again.\n\n" +
		strings.Join(parts, "\n\n") + "\n</workspace_context>"

	return text
}

// includes are the files the instruction files include with an @ line,
// each under its path. The system prompt holds each instruction file
// under a "## <path>" header (instructions.Assemble), and a relative
// include is resolved against that file's directory.
func includes(systemPrompt string) string {
	var b strings.Builder
	var seen []string
	dir := ""
	sc := bufio.NewScanner(strings.NewReader(systemPrompt))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if p, ok := strings.CutPrefix(line, "## "); ok && filepath.IsAbs(p) {
			dir = filepath.Dir(p)

			continue
		}
		p, ok := strings.CutPrefix(line, "@")
		if !ok || p == "" || strings.ContainsAny(p, " \t") || dir == "" {
			continue
		}
		p = resolveInclude(dir, p)
		if slices.Contains(seen, p) {
			continue
		}
		seen = append(seen, p)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		body := strings.TrimSpace(string(data))
		if b.Len()+len(body) > primedIncludes {
			continue
		}
		fmt.Fprintf(&b, "Included by AGENTS.md (@%s), so you need not read it:\n%s\n\n", p, body)
	}

	return strings.TrimSpace(b.String())
}

// resolveInclude makes an include's path absolute: ~/ from the home
// directory, a relative one from dir.
func resolveInclude(dir, p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}

	return filepath.Join(dir, p)
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
	case len(lines) > primedStatus:
		lines = append(lines[:primedStatus], fmt.Sprintf("(%d more)", len(lines)-primedStatus))
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
		if i == primedListing || b.Len()+len(line) > limit {
			fmt.Fprintf(&b, "\n(%d more entries)", len(order)-i)

			break
		}
		b.WriteString(line)
	}

	return b.String()
}

// git runs a read-only git command in the workspace.
func git(ctx context.Context, workspace string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, primedGitWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", workspace, "--no-optional-locks"}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to run git %s: %w", args[0], err)
	}

	return string(out), nil
}

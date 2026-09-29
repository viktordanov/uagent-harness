// Package codereview is Codex's /review: what to review (a Target), the
// reviewer's prompt for it, the reviewer's system prompt, the findings it
// answers with, and the messages that hand them to the main agent. The
// session runs the reviewer (internal/session/review.go) as a read-only
// subagent (internal/agents/review.go).
//
// The prompts in prompts/ are Codex's, verbatim (rust-v0.156.1,
// codex-rs/prompts/templates/review), and the prompt texts, hints, and
// output format in this package follow codex-rs/prompts/src/review_request.rs
// and codex-rs/protocol/src/review_format.rs. Apache License 2.0, Copyright
// 2025 OpenAI; see prompts/LICENSE-codex.
package codereview

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"

	"github.com/viktordanov/uagent-harness/internal/gitdiff"
)

var (
	//go:embed prompts/rubric.md
	rubric string
	//go:embed prompts/exit_success.xml
	exitSuccess string
	//go:embed prompts/exit_interrupted.xml
	exitInterrupted string
)

// Instructions is the reviewer's system prompt, Codex's review rubric,
// which replaces the base instructions for the review.
func Instructions() string { return rubric }

// Kind is what a review looks at.
type Kind string

// Codex's review targets.
const (
	Uncommitted Kind = "uncommitted"
	BaseBranch  Kind = "branch"
	Commit      Kind = "commit"
	Custom      Kind = "custom"
)

// Target is what to review: the uncommitted changes, the changes against a
// base branch, one commit (with its subject, when known), or custom
// instructions.
type Target struct {
	Kind         Kind
	Branch       string
	SHA, Title   string
	Instructions string
}

// Codex's prompts for each target (review_request.rs).
const (
	uncommittedPrompt      = "Review the current code changes (staged, unstaged, and untracked files) and provide prioritized findings."
	baseBranchPrompt       = "Review the code changes against the base branch '%[1]s'. The merge base commit for this comparison is %[2]s. Run `git diff %[2]s` to inspect the changes relative to %[1]s. Provide prioritized, actionable findings."
	baseBranchPromptBackup = "Review the code changes against the base branch '%[1]s'. Start by finding the merge diff between the current branch and %[1]s's upstream e.g. (`git merge-base HEAD \"$(git rev-parse --abbrev-ref \"%[1]s@{upstream}\")\"`), then run `git diff` against that SHA to see what changes we would merge into the %[1]s branch. Provide prioritized, actionable findings."
	commitPromptWithTitle  = "Review the code changes introduced by commit %s (\"%s\"). Provide prioritized, actionable findings."
	commitPrompt           = "Review the code changes introduced by commit %s. Provide prioritized, actionable findings."
)

// ErrEmpty is Codex's refusal of custom instructions with no text.
var ErrEmpty = errors.New("Review prompt cannot be empty") //nolint:staticcheck // Codex's message, word for word

// Prompt is the reviewer's first message for the target, in the work tree
// of dir. A base branch's prompt names the merge base, which it finds with
// git; without one it tells the reviewer to find it, as Codex does.
func Prompt(ctx context.Context, dir string, t Target) (string, error) {
	switch t.Kind {
	case Uncommitted:
		return uncommittedPrompt, nil
	case BaseBranch:
		base, err := gitdiff.MergeBase(ctx, dir, t.Branch)
		if err != nil {
			return "", fmt.Errorf("failed to find the merge base: %w", err)
		}
		if base == "" {
			return fmt.Sprintf(baseBranchPromptBackup, t.Branch), nil
		}

		return fmt.Sprintf(baseBranchPrompt, t.Branch, base), nil
	case Commit:
		if t.Title != "" {
			return fmt.Sprintf(commitPromptWithTitle, t.SHA, t.Title), nil
		}

		return fmt.Sprintf(commitPrompt, t.SHA), nil
	case Custom:
	}
	text := strings.TrimSpace(t.Instructions)
	if text == "" {
		return "", ErrEmpty
	}

	return text, nil
}

// Hint is what the TUI says is being reviewed, Codex's user_facing_hint:
// "current changes", "changes against 'main'", "commit 1a2b3c4: Fix it",
// or the custom instructions.
func (t Target) Hint() string {
	switch t.Kind {
	case Uncommitted:
		return "current changes"
	case BaseBranch:
		return fmt.Sprintf("changes against '%s'", t.Branch)
	case Commit:
		short := t.SHA[:min(7, len(t.SHA))]
		if t.Title != "" {
			return fmt.Sprintf("commit %s: %s", short, t.Title)
		}

		return "commit " + short
	case Custom:
	}

	return strings.TrimSpace(t.Instructions)
}

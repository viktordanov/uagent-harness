package state

import (
	"strings"

	"github.com/viktordanov/uagent-harness/internal/codereview"
	"github.com/viktordanov/uagent-harness/internal/gitdiff"
)

// The /review menu is Codex's review popup as the command menu: after
// "/review " it offers the presets, after "/review branch " the local
// branches, and after "/review commit " the recent commits. The lists are
// read once per /review (EffLoadReviewTargets).

type (
	// EffLoadReviewTargets reads the branches and recent commits of the
	// work tree that holds Dir.
	EffLoadReviewTargets struct{ Dir string }
	// ReviewTargetsLoaded carries them; Err says why they are missing.
	ReviewTargetsLoaded struct {
		Branches gitdiff.Branches
		Commits  []gitdiff.Commit
		Err      error
	}
)

func (EffLoadReviewTargets) effect() {}

// reviewPresets are Codex's review presets, as menu entries. The custom
// entry only explains: instructions are typed after /review.
var reviewPresets = []Suggestion{
	{Label: "branch", Help: "review against a base branch (PR style)", Draft: "/review branch "},
	{Label: "uncommitted", Help: "review uncommitted changes", Draft: "/review uncommitted"},
	{Label: "commit", Help: "review a commit", Draft: "/review commit "},
	{Label: "<instructions>", Help: "custom review instructions: type them after /review", Draft: "/review "},
}

// reviewSuggestions completes /review's argument.
func (s State) reviewSuggestions(arg string) []Suggestion {
	word, rest, more := strings.Cut(arg, " ")
	if !more {
		var out []Suggestion
		for _, p := range reviewPresets {
			if arg == "" || (p.Label != arg && strings.HasPrefix(p.Label, arg)) {
				out = append(out, p)
			}
		}

		return out
	}
	t := s.Menu.Review
	if t == nil {
		return nil
	}
	switch codereview.Kind(word) {
	case codereview.BaseBranch:
		return branchSuggestions(t.Branches, rest)
	case codereview.Commit:
		return commitSuggestions(t.Commits, rest)
	case codereview.Uncommitted, codereview.Custom:
	}

	return nil
}

func branchSuggestions(b gitdiff.Branches, query string) []Suggestion {
	var out []Suggestion
	for _, name := range b.Names {
		if name == query || !strings.Contains(name, query) {
			continue
		}
		sg := Suggestion{Label: name, Draft: "/review branch " + name}
		if name == b.Current {
			sg.Help = "checked out"
		}
		out = append(out, sg)
	}

	return out
}

func commitSuggestions(commits []gitdiff.Commit, query string) []Suggestion {
	var out []Suggestion
	q := strings.ToLower(query)
	for _, c := range commits {
		if c.SHA == query || (!strings.HasPrefix(c.SHA, query) && !strings.Contains(strings.ToLower(c.Subject), q)) {
			continue
		}
		out = append(out, Suggestion{Label: c.Subject, Help: c.SHA[:min(7, len(c.SHA))], Draft: "/review commit " + c.SHA})
	}

	return out
}

// loadReviewTargets starts reading the branches and commits when the
// draft reaches /review's argument and they are not loaded.
func (s *State) loadReviewTargets(draft string) Effect {
	if !strings.HasPrefix(draft, "/review ") || s.Menu.Review != nil || s.Menu.reviewLoading {
		return nil
	}
	s.Menu.reviewLoading = true

	return EffLoadReviewTargets{Dir: s.Settings.Workspace}
}

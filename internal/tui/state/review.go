package state

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/codereview"
	"github.com/viktordanov/uah/internal/gitdiff"
	"github.com/viktordanov/uah/internal/session"
)

// /diff and /review, after Codex's: /diff shows the work tree's changes
// in the transcript, and /review runs a read-only reviewer over a target
// (the menu after "/review " picks it, reviewmenu.go) and shows its
// findings.

// Review is a /review's item: what it looks at, its reviewer's latest
// tool call while it runs, and then its findings, or how it ended.
type Review struct {
	Hint    string
	Running bool
	Started time.Time
	Ended   time.Time
	// Doing is the reviewer's latest tool call while it runs.
	Doing       string
	Output      codereview.Output
	Interrupted bool
	Err         string
	// Workspace is the session's, so paths can show relative to it.
	Workspace string
}

type (
	// EffDiff collects the changes of the work tree that holds Dir.
	EffDiff struct{ Dir string }
	// EffReview runs a review of Target (Session.Review).
	EffReview struct{ Target codereview.Target }
	// DiffShown carries /diff's changes, or why there are none.
	DiffShown struct {
		Diff gitdiff.Diff
		Err  error
	}
)

func (EffDiff) effect()   {}
func (EffReview) effect() {}

// cmdReviewName is /review's name, which the menu completes.
const cmdReviewName = "review"

func cmdDiff(s *State, _ string) []Effect { return []Effect{EffDiff{Dir: s.Settings.Workspace}} }

// cmdReview runs a review of the target its arguments name: uncommitted,
// branch <name>, commit <sha>, or anything else as custom instructions,
// as Codex's /review <text> does. Without a complete target it puts the
// draft back for the menu to finish.
func cmdReview(s *State, args string) []Effect {
	if s.Reviewing != "" {
		s.notice(session.LevelWarning, "a review is already running; esc esc stops it")

		return nil
	}
	word, rest, _ := strings.Cut(args, " ")
	rest = strings.TrimSpace(rest)
	var t codereview.Target
	switch {
	case args == "", (word == string(codereview.BaseBranch) || word == string(codereview.Commit)) && rest == "":
		return s.setDraft(strings.TrimSpace("/review "+word) + " ")
	case args == string(codereview.Uncommitted):
		t = codereview.Target{Kind: codereview.Uncommitted}
	case word == string(codereview.BaseBranch):
		t = codereview.Target{Kind: codereview.BaseBranch, Branch: rest}
	case word == string(codereview.Commit):
		t = s.commitTarget(rest)
	default:
		t = codereview.Target{Kind: codereview.Custom, Instructions: args}
	}
	s.Menu.Review = nil // the branches and commits are read again next time

	return []Effect{EffReview{Target: t}}
}

// commitTarget is the commit a SHA or its prefix names, with its subject
// when the menu listed it.
func (s *State) commitTarget(ref string) codereview.Target {
	if s.Menu.Review != nil {
		for _, c := range s.Menu.Review.Commits {
			if strings.HasPrefix(c.SHA, ref) {
				return codereview.Target{Kind: codereview.Commit, SHA: c.SHA, Title: c.Subject}
			}
		}
	}

	return codereview.Target{Kind: codereview.Commit, SHA: ref}
}

// onReview handles /diff's result, the review menu's lists, and the
// session's review events, and reports whether ev was one.
func (s *State) onReview(ev any) bool {
	switch e := ev.(type) {
	case DiffShown:
		s.showDiff(e)
	case ReviewTargetsLoaded:
		s.Menu.Review, s.Menu.reviewLoading = &e, false
	case session.ReviewStarted:
		s.Reviewing = e.ID
		s.put(Item{Kind: KindReview, Key: "review:" + e.ID, Review: &Review{Hint: e.Hint, Running: true, Started: e.At, Workspace: s.Settings.Workspace}})
	case session.ReviewActivity:
		if c, ok := e.Event.(core.ToolCalled); ok {
			s.updateReview(e.ID, func(r *Review) { r.Doing = s.callLabel(c.Name, c.Label) })
		}
	case session.ReviewFinished:
		if s.Reviewing == e.ID {
			s.Reviewing = ""
		}
		s.updateReview(e.ID, func(r *Review) {
			r.Running, r.Ended, r.Doing = false, e.At, ""
			r.Output, r.Interrupted, r.Err = e.Output, e.Interrupted, e.Err
		})
	default:
		return false
	}

	return true
}

// updateReview changes a review's item in place; the item's Review is
// copied, so an earlier state keeps its own.
func (s *State) updateReview(id string, fn func(*Review)) {
	s.update("review:"+id, func(it *Item) {
		if it.Review == nil {
			return
		}
		r := *it.Review
		fn(&r)
		it.Review = &r
	})
}

// showDiff puts /diff's changes in the transcript, or says, in Codex's
// words, why there are none.
func (s *State) showDiff(e DiffShown) {
	switch {
	case errors.Is(e.Err, gitdiff.ErrNotRepo):
		s.notice(session.LevelWarning, "/diff — not inside a git repository")
	case e.Err != nil:
		s.notice(session.LevelError, fmt.Sprintf("Failed to compute diff: %v", e.Err))
	case len(e.Diff.Files) == 0 && e.Diff.MoreUntracked == 0:
		s.notice(session.LevelInfo, "No changes detected.")
	default:
		d := e.Diff
		s.put(Item{Kind: KindDiff, Key: s.nextKey("diff"), GitDiff: &d})
	}
}

// ReviewRunning reports whether a /review is running, which esc esc stops.
func (s State) ReviewRunning() bool { return s.Reviewing != "" }

// reviewNote reads the message that handed a review to the main agent as
// a line of the transcript, since the user did not write it.
func reviewNote(text string) (string, bool) {
	if !codereview.IsExitMessage(text) {
		return "", false
	}

	return "the review went to the main agent with this message", true
}

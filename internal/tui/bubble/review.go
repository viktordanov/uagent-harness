package bubble

import (
	tea "charm.land/bubbletea/v2"

	"github.com/viktordanov/uagent-harness/internal/gitdiff"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// runReview runs /diff's and /review's effects: git reads off the update
// loop, and the review through the session, whose events report it.
func (m Model) runReview(e state.Effect) (tea.Cmd, bool) {
	ctx, sess := m.ctx, m.sess
	switch e := e.(type) {
	case state.EffDiff:
		return func() tea.Msg {
			d, err := gitdiff.Collect(ctx, e.Dir)

			return state.DiffShown{Diff: d, Err: err}
		}, true
	case state.EffLoadReviewTargets:
		return func() tea.Msg {
			branches, err := gitdiff.ListBranches(ctx, e.Dir)
			if err != nil {
				return state.ReviewTargetsLoaded{Err: err}
			}
			commits, err := gitdiff.ListCommits(ctx, e.Dir, gitdiff.RecentCommits)

			return state.ReviewTargetsLoaded{Branches: branches, Commits: commits, Err: err}
		}, true
	case state.EffReview:
		return func() tea.Msg {
			if sess == nil {
				return state.Failed{Err: errNoSession}
			}
			if err := sess.Review(ctx, e.Target); err != nil {
				return state.Failed{Err: err}
			}

			return nil
		}, true
	}

	return nil, false
}

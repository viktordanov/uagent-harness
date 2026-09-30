package session

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/codereview"
)

// ErrNoReview means the session's engine cannot run a reviewer.
var ErrNoReview = errors.New("/review needs the embedded engine")

// ReviewRequest is one review for a Reviewer to run: a fresh read-only
// session beside the parent, with Codex's rubric as its system prompt and
// Prompt as its only message.
type ReviewRequest struct {
	ParentID string
	// Settings are the parent's settings now; the reviewer changes the
	// mode, the system prompt, and the model.
	Settings Settings
	Prompt   string
	// Activity gets the reviewer's tool events as they happen.
	Activity func(core.Event)
}

// Reviewer runs reviews; internal/agents implements it. Review returns the
// reviewer's last message, or an error when it failed or ctx ended.
type Reviewer interface {
	Review(ctx context.Context, req ReviewRequest) (string, error)
}

// ReviewStarted means a /review began: Hint says what it looks at.
type ReviewStarted struct {
	At   time.Time
	ID   string
	Hint string
}

// ReviewActivity is one of the reviewer's tool events.
type ReviewActivity struct {
	At    time.Time
	ID    string
	Event core.Event
}

// ReviewFinished ends a review: its output, or Interrupted, or the error
// that stopped it (Err).
type ReviewFinished struct {
	At          time.Time
	ID          string
	Output      codereview.Output
	Interrupted bool
	Err         string
}

func (e ReviewStarted) OccurredAt() time.Time  { return e.At }
func (e ReviewActivity) OccurredAt() time.Time { return e.At }
func (e ReviewFinished) OccurredAt() time.Time { return e.At }

type cmdReview struct {
	hint   string
	cancel context.CancelFunc
}

// reviewStart is a started review: its ID and the settings it starts from.
type reviewStart struct {
	id       string
	settings Settings
}

// Review runs Codex's /review of the target: a subagent with Codex's
// review rubric, read-only, reviews the changes and answers with findings.
// It returns when the review ends: when the reviewer answers, ctx ends, an
// interrupt stops it, or the session closes. The findings reach the main
// agent as Codex hands them over, in a <user_action> message that goes
// with the next message (as Inject does); an interrupted review sends
// Codex's interrupted form. One review runs at a time.
func (s *Session) Review(ctx context.Context, target codereview.Target) error {
	reviewer, ok := s.reviewer()
	if !ok {
		return ErrNoReview
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel) //nolint:contextcheck // closing the session also stops the review
	defer stop()
	start, err := call[reviewStart](s, cmdReview{hint: target.Hint(), cancel: cancel})
	if err != nil {
		return err
	}
	fin := ReviewFinished{ID: start.id}
	prompt, err := codereview.Prompt(ctx, start.settings.Workspace, target)
	if err == nil {
		var answer string
		answer, err = reviewer.Review(ctx, ReviewRequest{
			ParentID: s.id, Settings: start.settings, Prompt: prompt,
			Activity: func(e core.Event) {
				s.post(evNotify{event: ReviewActivity{At: time.Now(), ID: start.id, Event: e}})
			},
		})
		fin.Output = codereview.Parse(answer)
	}
	switch {
	case ctx.Err() != nil:
		fin.Interrupted, fin.Output = true, codereview.Output{}
	case err != nil:
		fin.Err = err.Error()
	}
	s.post(evDo(func() { s.onReviewDone(fin) }))

	return nil
}

// reviewer is the engine's Reviewer, if it has one.
func (s *Session) reviewer() (Reviewer, bool) {
	e, ok := s.eng.(SubagentsEngine)
	if !ok {
		return nil, false
	}
	r, ok := e.Subagents().(Reviewer)

	return r, ok
}

// onReview starts a review unless one is running; the session's
// interrupt stops it.
func (s *Session) onReview(c cmdReview) (reviewStart, error) {
	if s.reviewStop != nil {
		return reviewStart{}, errors.New("a review is already running")
	}
	id := uuid.NewString()
	s.reviewStop = c.cancel
	s.emit(ReviewStarted{At: time.Now(), ID: id, Hint: c.hint})

	return reviewStart{id: id, settings: s.settings}, nil
}

// onReviewDone reports the review and holds Codex's exit message for the
// next run. A failed review sends nothing: Codex's thread gets no review
// then either.
func (s *Session) onReviewDone(fin ReviewFinished) {
	s.reviewStop = nil
	fin.At = time.Now()
	s.emit(fin)
	if s.state != StateClosed && fin.Err == "" {
		s.held = append(s.held, core.UserInput{ID: fin.ID, Text: codereview.ExitMessage(fin.Output, fin.Interrupted)})
	}
}

// stopReview stops a running review (an interrupt).
func (s *Session) stopReview() {
	if s.reviewStop != nil {
		s.reviewStop()
	}
}

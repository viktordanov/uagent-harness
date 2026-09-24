package state

import (
	"slices"
	"strings"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// Approval is a command waiting for the user's approval. The first pending
// one shows as the approval overlay.
type Approval struct {
	ID            string
	Command       string
	Justification string
	// Escalation is true when the command would run outside the sandbox.
	Escalation bool
	// Prefix, when set, offers "don't ask again" for it.
	Prefix []string
	// Answered hides the choices once the answer is on its way.
	Answered bool
}

// Answer is the user's choice in the approval overlay.
type Answer struct{ Answer approval.Answer }

// EffResolve answers a pending approval.
type EffResolve struct {
	ID     string
	Answer approval.Answer
}

func (EffResolve) effect() {}

// PendingApproval is the approval the overlay shows, if any.
func (s State) PendingApproval() (Approval, bool) {
	if len(s.Approvals) == 0 {
		return Approval{}, false
	}

	return s.Approvals[0], true
}

func (s *State) requestApproval(e session.ApprovalRequested) {
	s.Approvals = append(s.Approvals, Approval{
		ID: e.ID, Command: e.Command, Justification: e.Justification, Escalation: e.Escalation, Prefix: e.ProposedPrefix,
	})
	s.Scroll = 0
}

// resolveApproval closes the approval and records the answer in the
// transcript, as Codex does.
func (s *State) resolveApproval(e session.ApprovalResolved) {
	i := slices.IndexFunc(s.Approvals, func(a Approval) bool { return a.ID == e.ID })
	if i < 0 {
		return
	}
	a := s.Approvals[i]
	s.Approvals = slices.Delete(s.Approvals, i, i+1)
	command := oneLine(a.Command)
	switch e.Decision {
	case approval.Approve:
		s.notice(session.LevelInfo, "✔ approved: "+command)
	case approval.ApprovePrefix:
		s.notice(session.LevelInfo, "✔ approved, and from now on commands that start with `"+strings.Join(a.Prefix, " ")+"`: "+command)
	case approval.Decline:
		s.notice(session.LevelWarning, "✗ declined: "+command)
	}
}

// answer sends the user's choice for the pending approval once.
func (s *State) answer(e Answer) (State, []Effect) {
	if len(s.Approvals) == 0 || s.Approvals[0].Answered {
		return *s, nil
	}
	a := &s.Approvals[0]
	if e.Answer == approval.ApprovePrefix && len(a.Prefix) == 0 {
		return *s, nil
	}
	a.Answered = true

	return *s, []Effect{EffResolve{ID: a.ID, Answer: e.Answer}}
}

func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }

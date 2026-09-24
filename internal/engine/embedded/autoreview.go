package embedded

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/review"
)

// How much of the session the auto-reviewer sees; the reviewer trims it to
// its own budget.
const (
	keepUserMessages = 20
	keepToolCalls    = 20
)

// transcript keeps what the auto-reviewer needs from the session's events:
// the user's messages (trusted) and the latest tool calls without their
// output (untrusted).
type transcript struct {
	mu     sync.Mutex
	users  []string
	calls  []recentCall
	onUser func() // a new user turn closes the reviewer's circuit breaker
}

type recentCall struct {
	id   string
	call review.ToolCall
}

func newTranscript() *transcript { return &transcript{} }

func (t *transcript) observe(e core.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch v := e.(type) {
	case core.UserMessage:
		t.users = keepLast(append(t.users, v.Text), keepUserMessages)
		if t.onUser != nil {
			t.onUser()
		}
	case core.ToolCalled:
		t.calls = keepLast(append(t.calls, recentCall{id: v.CallID, call: review.ToolCall{Name: v.Name, Arguments: v.Arguments}}), keepToolCalls)
	case core.ToolFinished:
		for i := range t.calls {
			if t.calls[i].id == v.CallID {
				t.calls[i].call.Status = v.Detail
			}
		}
	}
}

func (t *transcript) snapshot() (users []string, calls []review.ToolCall) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, c := range t.calls {
		calls = append(calls, c.call)
	}

	return append([]string(nil), t.users...), calls
}

func keepLast[T any](s []T, n int) []T { return s[max(len(s)-n, 0):] }

// reviewedAsk puts the auto-reviewer in front of the session's ask, as
// Codex's auto_review does: allow runs the action, deny refuses it with the
// reviewer's reason, and ask_user (the circuit breaker is open) leaves it to
// PermissionRequest hooks and the user. In Auto mode the reviewer decides
// alone, also when approvals_reviewer is user: ask_user declines with its
// reason, as Codex's "Approve for me" never asks. Without auto-review and
// outside Auto mode, the session's ask answers.
func (w *wiring) reviewedAsk(sw *switcher, req core.Request) approval.Ask {
	rv := review.New(sw.direct(), w.e.cfg.Review)
	t := w.e.transcript(req.SessionID)
	t.mu.Lock()
	t.onUser = rv.Reset
	t.mu.Unlock()
	next, emit, mode, always := w.ask, w.emit, w.mode, w.e.cfg.AutoReview

	return func(ctx context.Context, p approval.Prompt) approval.Answer {
		alone := mode.get().ReviewerDecides()
		if !alone && !always {
			return askNext(ctx, next, p)
		}
		v, err := rv.Review(ctx, reviewRequest(t, req, p, mode.get()))
		if err != nil {
			return approval.Decline
		}
		if emit != nil {
			emit(engine.AutoReviewed{At: time.Now(), Command: p.Command, Outcome: string(v.Outcome), Risk: string(v.Risk), Reason: v.Reason})
		}
		switch v.Outcome {
		case review.Allow:
			return approval.Approve
		case review.Deny:
			return approval.DeclineBecause("the auto-reviewer denied this (" + string(v.Risk) + " risk): " + v.Reason + ". Do not retry it; tell the user if it is needed.")
		case review.AskUser:
		}
		if alone {
			return approval.DeclineBecause("in auto mode the auto-reviewer decides, and it did not allow this: " + v.Reason + ". Do not retry it; tell the user if it is needed.")
		}

		return askNext(ctx, next, p)
	}
}

// askNext asks the session, or declines when no user can answer.
func askNext(ctx context.Context, next approval.Ask, p approval.Prompt) approval.Answer {
	if next == nil {
		return approval.DeclineBecause("the auto-reviewer left this to the user, and no user can approve it in this headless run.")
	}

	return next(ctx, p)
}

// reviewRequest is what the reviewer judges: the action with the
// session's recent messages and tool calls.
func reviewRequest(t *transcript, req core.Request, p approval.Prompt, mode approval.Mode) review.Request {
	users, calls := t.snapshot()
	action := review.Action{Tool: "Bash", Command: p.Command, Cwd: p.Cwd, Justification: p.Justification}
	if mode != "" {
		action.SandboxMode = string(mode.Sandbox())
	}
	if name, args, _ := strings.Cut(p.Command, " "); strings.HasPrefix(name, "mcp__") {
		action.Tool, action.Command = name, args
	}
	if p.Tool != "" {
		action.Tool, action.Command = p.Tool, string(p.Input)
	}
	if p.Escalation {
		action.SandboxPermissions = permEscalated
	}

	return review.Request{Action: action, UserMessages: users, RecentCalls: calls, SessionID: req.SessionID}
}

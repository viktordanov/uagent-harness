package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/hooks"
)

// ApprovalRequested asks the user to approve a command. The session waits
// for Resolve with its ID; the agent waits with it.
type ApprovalRequested struct {
	At            time.Time
	ID            string
	Command       string
	Cwd           string
	Justification string
	// Escalation is true when the command would run outside the sandbox;
	// false when a rule asks for approval of a sandboxed command.
	Escalation bool
	// ProposedPrefix, when set, can be allowed from now on.
	ProposedPrefix []string
}

// ApprovalResolved ends an approval: the user answered, or it was
// canceled (Decline) by an interrupt, the run's end, or Close.
type ApprovalResolved struct {
	At       time.Time
	ID       string
	Decision approval.Answer
}

func (e ApprovalRequested) OccurredAt() time.Time { return e.At }
func (e ApprovalResolved) OccurredAt() time.Time  { return e.At }

// Loop messages for approvals.
type (
	cmdAsk struct {
		id     string
		prompt approval.Prompt
		reply  chan approval.Answer
	}
	cmdAskGone struct{ id string }
	cmdResolve struct {
		id     string
		answer approval.Answer
	}
)

// Resolve answers a pending approval.
func (s *Session) Resolve(id string, answer approval.Answer) error {
	_, err := call[struct{}](s, cmdResolve{id: id, answer: answer})

	return err
}

// askFunc is how runs ask the user: through the session's events when the
// session is interactive, nil (deny) otherwise.
func (s *Session) askFunc() approval.Ask {
	hooked := s.hooks.runner.Has(hooks.PermissionRequest, "")
	if !s.interactive && !hooked {
		return nil
	}
	base := s.hookInput(hooks.PermissionRequest) // read on the loop goroutine

	return func(ctx context.Context, p approval.Prompt) approval.Answer {
		if hooked {
			d := s.hooks.runner.Run(ctx, permissionInput(base, p))
			switch {
			case d.Block:
				return approval.Decline
			case d.Allow:
				return approval.Approve
			}
		}
		if !s.interactive {
			return approval.Decline
		}

		return s.ask(ctx, p)
	}
}

// permissionInput describes the prompt to a PermissionRequest hook as a tool
// call: an MCP tool by its name and arguments, anything else as Bash.
func permissionInput(in hooks.Input, p approval.Prompt) hooks.Input {
	name, args, _ := strings.Cut(p.Command, " ")
	if strings.HasPrefix(name, "mcp__") && json.Valid([]byte(args)) {
		in.ToolName, in.ToolInput = name, json.RawMessage(args)

		return in
	}
	input, err := json.Marshal(struct {
		Command       string `json:"command"`
		Justification string `json:"justification,omitempty"`
		Escalation    bool   `json:"sandbox_escalation,omitempty"`
	}{p.Command, p.Justification, p.Escalation})
	if err == nil {
		in.ToolName, in.ToolInput = "Bash", input
	}

	return in
}

// ask runs on the engine's goroutine: it hands the prompt to the loop and
// waits for the answer, declining when ctx ends or the session closes.
func (s *Session) ask(ctx context.Context, p approval.Prompt) approval.Answer {
	id := uuid.NewString()
	reply := make(chan approval.Answer, 1)
	select {
	case s.in <- cmdAsk{id: id, prompt: p, reply: reply}:
	case <-ctx.Done():
		return approval.Decline
	case <-s.done:
		return approval.Decline
	}
	select {
	case a := <-reply:
		return a
	case <-ctx.Done():
		s.post(cmdAskGone{id: id})

		return approval.Decline
	case <-s.done:
		return approval.Decline
	}
}

// onAsk records a pending approval and shows it, or declines at once while
// the run is stopping or the session closing.
func (s *Session) onAsk(c cmdAsk) {
	live := s.state == StateRunning || (s.state == StateStarting && !s.interruptWhenStarted)
	if s.closeReply != nil || !live {
		c.reply <- approval.Decline

		return
	}
	s.approvals[c.id] = c.reply
	p := c.prompt
	s.emit(ApprovalRequested{
		At: time.Now(), ID: c.id, Command: p.Command, Cwd: p.Cwd, Justification: p.Justification,
		Escalation: p.Escalation, ProposedPrefix: p.ProposedPrefix,
	})
}

// onResolve answers a pending approval.
func (s *Session) onResolve(c cmdResolve) error {
	if _, ok := s.approvals[c.id]; !ok {
		return fmt.Errorf("no pending approval %q", c.id)
	}
	s.answer(c.id, c.answer)

	return nil
}

// declinePending declines every pending approval, so a waiting run can stop.
func (s *Session) declinePending() {
	for id := range s.approvals {
		s.answer(id, approval.Decline)
	}
}

func (s *Session) answer(id string, a approval.Answer) {
	reply, ok := s.approvals[id]
	if !ok {
		return
	}
	delete(s.approvals, id)
	reply <- a
	s.emit(ApprovalResolved{At: time.Now(), ID: id, Decision: a})
}

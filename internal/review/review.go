// Package review is the automatic approval reviewer, Codex's "auto-review"
// (guardian): one model call judges an action that needs approval, from the
// user's messages (trusted), the recent tool calls without their output
// (untrusted), and the action itself. It fails closed, and a circuit
// breaker hands the decision back to the user after repeated denials.
//
// The prompts in prompts/ are Codex's (rust-v0.156.1,
// codex-rs/prompts/templates/guardian), Apache License 2.0, Copyright 2025
// OpenAI, trimmed for a reviewer without tools; see prompts/LICENSE-codex.
package review

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/llm"

	"github.com/viktordanov/uagent-harness/internal/llmcall"
)

// DefaultTimeout bounds one review, as Codex's REVIEW_TIMEOUT does.
const DefaultTimeout = 90 * time.Second

// maxAttempts is how often a review runs when the model's answer does not
// parse, as Codex's MAX_REVIEW_ATTEMPTS. Transport errors are retried by
// the client itself.
const maxAttempts = 3

// Outcome is the reviewer's decision.
type Outcome string

const (
	// Allow runs the action.
	Allow Outcome = "allow"
	// Deny refuses the action; the reason goes back to the model.
	Deny Outcome = "deny"
	// AskUser means the circuit breaker is open: the user decides (headless
	// runs deny).
	AskUser Outcome = "ask_user"
)

// Risk is the reviewer's risk level.
type Risk string

// The risk levels, as in Codex.
const (
	RiskLow      Risk = "low"
	RiskMedium   Risk = "medium"
	RiskHigh     Risk = "high"
	RiskCritical Risk = "critical"
)

// Action is what the agent wants to do.
type Action struct {
	// Tool is the tool name, such as Bash.
	Tool    string
	Command string
	Cwd     string
	// SandboxMode is the session's sandbox mode, such as workspace-write.
	SandboxMode string
	// SandboxPermissions is what the call asked for, such as
	// require_escalated.
	SandboxPermissions string
	// Justification is the agent's reason for asking.
	Justification string
	// Denied is what the sandbox denied on the first run, if it ran.
	Denied string
	// Rule is the rule context, such as the rule that asked for approval.
	Rule string
}

// ToolCall is one recent tool call, without its output.
type ToolCall struct {
	Name      string
	Arguments string
	// Status is a short result without output, such as "exit 1".
	Status string
}

// Request is one review.
type Request struct {
	Action Action
	// UserMessages are the session's user messages, oldest first. They are
	// the only trusted input.
	UserMessages []string
	// RecentCalls are the latest tool calls, oldest first. They are
	// untrusted.
	RecentCalls []ToolCall
	// Policy replaces the default security policy when set, as Codex's
	// [auto_review] policy does.
	Policy string
	// SessionID keys the provider's prompt cache.
	SessionID string
}

// Verdict is the result of a review.
type Verdict struct {
	Outcome Outcome
	Risk    Risk
	// Authorization is the reviewer's user_authorization: unknown, low,
	// medium, or high.
	Authorization string
	Reason        string
	// Failed means the review did not finish (an error, a timeout, or an
	// answer that did not parse), so the verdict is a fail-closed deny.
	Failed bool
	Usage  llm.Usage
}

// Config is the reviewer's model, budget, and policy.
type Config struct {
	Model  string
	Effort llm.ReasoningEffort
	// Policy replaces the default security policy for every request that
	// sets none; PolicyFile is the file it was read from ([review]
	// policy_file), for display.
	Policy     string
	PolicyFile string
	// Timeout bounds a review (DefaultTimeout when zero).
	Timeout time.Duration
	Limits  Limits
}

// Reviewer reviews actions over one model adapter. It is safe for
// concurrent use.
type Reviewer struct {
	adapter llm.Adapter
	cfg     Config

	mu      sync.Mutex
	breaker Breaker
}

// New returns a reviewer that calls adapter.
func New(adapter llm.Adapter, cfg Config) *Reviewer {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.Limits == (Limits{}) {
		cfg.Limits = DefaultLimits
	}

	return &Reviewer{adapter: adapter, cfg: cfg}
}

// Review judges the request. A failed review is a deny with Failed set;
// the error is only for a cancelled ctx. When the circuit breaker is open,
// it returns AskUser without calling the model.
func (r *Reviewer) Review(ctx context.Context, req Request) (Verdict, error) {
	r.mu.Lock()
	open := r.breaker.Open()
	r.mu.Unlock()
	if open {
		return Verdict{Outcome: AskUser, Reason: "auto-review denied too many actions in a row; the user decides"}, nil
	}
	v := r.review(ctx, req)
	if ctx.Err() != nil {
		return Verdict{}, fmt.Errorf("failed to review: %w", ctx.Err())
	}
	r.mu.Lock()
	r.breaker.Record(v.Outcome == Deny)
	r.mu.Unlock()

	return v, nil
}

// Reset closes the circuit breaker, as Codex does at each new turn.
func (r *Reviewer) Reset() {
	r.mu.Lock()
	r.breaker = Breaker{}
	r.mu.Unlock()
}

func (r *Reviewer) review(ctx context.Context, req Request) Verdict {
	call := llmcall.Request{
		Model: r.cfg.Model, Effort: r.cfg.Effort, Instructions: Instructions(cmp.Or(req.Policy, r.cfg.Policy)),
		Input:   []llm.Item{llmcall.Message(llm.RoleUser, Render(req, r.cfg.Limits))},
		Timeout: r.cfg.Timeout,
	}
	if req.SessionID != "" {
		call.CacheKey = "uah-review-" + req.SessionID
	}
	deadline := time.Now().Add(r.cfg.Timeout)
	var usage llm.Usage
	var err error
	for range maxAttempts {
		call.Timeout = time.Until(deadline)
		var res llmcall.Result
		res, err = llmcall.Call(ctx, r.adapter, call)
		usage = addUsage(usage, res.Usage)
		if err != nil && !errors.Is(err, llmcall.ErrNoText) {
			break
		}
		if err == nil {
			var v Verdict
			if v, err = Parse(res.Text); err == nil {
				v.Usage = usage

				return v
			}
		}
		if time.Until(deadline) <= 0 {
			break
		}
	}

	return failed(err, usage)
}

// failed is the fail-closed verdict for a review that did not finish.
func failed(err error, usage llm.Usage) Verdict {
	reason := "auto-review failed, so the action is denied: " + err.Error()
	if errors.Is(err, context.DeadlineExceeded) {
		reason = "auto-review did not finish before its deadline, so the action is denied. " +
			"Do not assume the action is unsafe based on the timeout alone; retry once, or ask the user."
	}

	return Verdict{Outcome: Deny, Risk: RiskHigh, Reason: reason, Failed: true, Usage: usage}
}

func addUsage(a, b llm.Usage) llm.Usage {
	a.InputTokens += b.InputTokens
	a.CachedInputTokens += b.CachedInputTokens
	a.OutputTokens += b.OutputTokens
	a.ReasoningTokens += b.ReasoningTokens

	return a
}

// Package hooks runs user commands at session events, with Claude Code's
// contract: the event as JSON on stdin; exit 0 continues (optionally with
// JSON on stdout), exit 2 blocks with stderr as the reason, and any other
// exit is reported and ignored.
package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Event names a point where hooks run.
type Event string

const (
	SessionStart     Event = "SessionStart"
	SessionEnd       Event = "SessionEnd"
	UserPromptSubmit Event = "UserPromptSubmit"
	PreToolUse       Event = "PreToolUse"
	PostToolUse      Event = "PostToolUse"
	Stop             Event = "Stop"
	// SubagentStop runs when a subagent finishes; a block with a reason
	// sends the reason to the subagent as its next message.
	SubagentStop Event = "SubagentStop"
	PreCompact   Event = "PreCompact"
	// PermissionRequest runs before the user is asked to approve a command
	// or an MCP call; "allow" or "deny" answers for the user.
	PermissionRequest Event = "PermissionRequest"
)

// Events are the supported events.
var Events = []Event{SessionStart, SessionEnd, UserPromptSubmit, PreToolUse, PostToolUse, Stop, SubagentStop, PreCompact, PermissionRequest}

const (
	// DefaultTimeout applies when a hook sets none.
	DefaultTimeout = 60 * time.Second
	// sessionEndTimeout caps SessionEnd hooks, so quitting stays quick.
	sessionEndTimeout = time.Second
)

// Source says which file a hook came from.
type Source string

const (
	SourceUser    Source = "user"
	SourceProject Source = "project"
)

// Hook is one configured command.
type Hook struct {
	Event Event
	// Matcher is a regular expression on the tool name for tool events; empty matches all.
	Matcher string
	Command string
	Timeout time.Duration
	Source  Source
}

// Outcome is how a hook run ended.
type Outcome string

const (
	OutcomeOK      Outcome = "ok"
	OutcomeBlocked Outcome = "blocked"
	OutcomeError   Outcome = "error"
	// OutcomeSkipped means a project hook that is not trusted yet.
	OutcomeSkipped Outcome = "skipped"
)

// Result is one hook run.
type Result struct {
	Hook     Hook
	Outcome  Outcome
	ExitCode int
	// Reason is stderr for exit 2, the error for OutcomeError, or why it was skipped.
	Reason   string
	Output   Output
	Stdout   string
	Duration time.Duration
}

// Runner runs the hooks of one session.
type Runner struct {
	hooks     []Hook
	trust     *Trust
	workspace string

	mu      sync.Mutex
	report  func(Result)
	skipped map[string]bool // untrusted commands already reported
}

// New validates the hooks and returns a runner. Project hooks run only when
// trust has approved their exact command.
func New(hooks []Hook, trust *Trust, workspace string) (*Runner, error) {
	for _, h := range hooks {
		if !slices.Contains(Events, h.Event) {
			return nil, fmt.Errorf("unknown hook event %q", h.Event)
		}
		if strings.TrimSpace(h.Command) == "" {
			return nil, fmt.Errorf("a %s hook has no command", h.Event)
		}
		if _, err := regexp.Compile(h.Matcher); err != nil {
			return nil, fmt.Errorf("invalid %s hook matcher %q: %w", h.Event, h.Matcher, err)
		}
	}

	return &Runner{hooks: hooks, trust: trust, workspace: workspace}, nil
}

// Hooks returns the configured hooks.
func (r *Runner) Hooks() []Hook {
	if r == nil {
		return nil
	}

	return slices.Clone(r.hooks)
}

// Trusted reports whether a hook may run.
func (r *Runner) Trusted(h Hook) bool {
	ok, _ := r.TrustState(h)

	return ok
}

// TrustState reports whether a hook may run, and if not, why: a project hook
// that was never trusted, or whose command or script changed since.
func (r *Runner) TrustState(h Hook) (bool, string) {
	if h.Source != SourceProject {
		return true, ""
	}
	if r.trust == nil {
		return false, ReasonUntrusted
	}

	return r.trust.Check(r.workspace, h.Command)
}

// Only returns a runner with this runner's hooks for the events, its trust,
// and its own OnResult, such as a subagent's session with only its tool
// hooks. A nil runner stays nil.
func (r *Runner) Only(events ...Event) *Runner {
	if r == nil {
		return nil
	}
	hooks := slices.DeleteFunc(slices.Clone(r.hooks), func(h Hook) bool { return !slices.Contains(events, h.Event) })

	return &Runner{hooks: hooks, trust: r.trust, workspace: r.workspace}
}

// OnResult sets a function that sees every result, from any goroutine.
func (r *Runner) OnResult(fn func(Result)) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report = fn
}

// Has reports whether any hook matches the event (and tool name, for tool events).
func (r *Runner) Has(event Event, tool string) bool {
	return len(r.matching(event, tool)) > 0
}

func (r *Runner) matching(event Event, tool string) []Hook {
	if r == nil {
		return nil
	}
	var out []Hook
	for _, h := range r.hooks {
		if h.Event != event {
			continue
		}
		if h.Matcher != "" && tool != "" && !regexp.MustCompile("^(?:"+h.Matcher+")$").MatchString(tool) {
			continue
		}
		out = append(out, h)
	}

	return out
}

// Run runs the matching hooks in order and returns their combined decision.
func (r *Runner) Run(ctx context.Context, in Input) Decision {
	var d Decision
	for _, h := range r.matching(in.Event, in.ToolName) {
		var res Result
		if ok, why := r.TrustState(h); ok {
			res = r.exec(ctx, h, in)
		} else {
			res = Result{Hook: h, Outcome: OutcomeSkipped, Reason: why}
		}
		r.mu.Lock()
		report := r.report
		if res.Outcome == OutcomeSkipped {
			if r.skipped == nil {
				r.skipped = map[string]bool{}
			}
			if r.skipped[h.Command] {
				report = nil // said once is enough
			}
			r.skipped[h.Command] = true
		}
		r.mu.Unlock()
		if report != nil {
			report(res)
		}
		d.add(in.Event, res)
		if d.Block {
			break
		}
	}

	return d
}

// Decision combines the results of one event's hooks.
type Decision struct {
	// Block stops the prompt or the tool call, or (for Stop) keeps the agent
	// going with Reason as the next message.
	Block  bool
	Reason string
	// Allow is a hook's "allow" permission decision (PreToolUse,
	// PermissionRequest).
	Allow bool
	// UpdatedInput replaces a PreToolUse tool call's arguments.
	UpdatedInput json.RawMessage
	// Context is text to add to a prompt (UserPromptSubmit, SessionStart).
	Context []string
	// Messages are systemMessage texts to show the user.
	Messages []string
}

func (d *Decision) add(event Event, res Result) {
	if res.Output.SystemMessage != "" {
		d.Messages = append(d.Messages, res.Output.SystemMessage)
	}
	switch res.Outcome {
	case OutcomeBlocked:
		d.Block, d.Reason = true, res.Reason

		return
	case OutcomeOK:
	case OutcomeError, OutcomeSkipped:
		return
	}
	out := res.Output
	if out.Continue != nil && !*out.Continue && event != Stop && event != SubagentStop {
		d.Block, d.Reason = true, firstNonEmpty(out.StopReason, "stopped by a hook")

		return
	}
	if out.Decision == "block" {
		d.Block, d.Reason = true, firstNonEmpty(out.Reason, "blocked by a hook")

		return
	}
	if s := out.HookSpecificOutput; s != nil && d.addSpecific(*s) {
		return
	}
	if (event == UserPromptSubmit || event == SessionStart) && out == (Output{}) && strings.TrimSpace(res.Stdout) != "" {
		d.Context = append(d.Context, strings.TrimSpace(res.Stdout)) // plain stdout is context, as in Claude Code
	}
}

// addSpecific applies hookSpecificOutput and reports whether it blocked.
func (d *Decision) addSpecific(s SpecificOutput) bool {
	switch s.PermissionDecision {
	case "deny", "ask":
		d.Block, d.Reason = true, firstNonEmpty(s.PermissionDecisionReason, "denied by a hook")

		return true
	case "allow":
		d.Allow = true
	}
	if len(s.UpdatedInput) > 0 {
		d.UpdatedInput = s.UpdatedInput
	}
	if s.AdditionalContext != "" {
		d.Context = append(d.Context, s.AdditionalContext)
	}

	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}

	return ""
}

package embedded

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/viktordanov/uah-core/harness/llm"
)

// Adaptive effort's routing: a request whose input since the model's last
// output is only tool results goes the setting's steps (1 or 2) below the
// effort the user picked, never below low. The first request and every
// request that carries a user message go at the user's effort. A
// benchmark of the rules (docs/design/agent-tuning.md) kept this one:
// lowering only some follow-ups changed the effort between requests more
// often, which costs the prompt cache more than the lower effort saves.

// Effort updates: for a model that takes them (effort_updates and the
// catalog's supports_reasoning_effort_updates, as in Codex), every request
// carries the session's base effort, its first, and the first turn and
// every turn whose effort differs from the one the history last set get a
// configuration_update item that sets it (effortUpdate): the baseline,
// the router's lower effort before a follow-up and the user's again before
// a user message, or a new /effort. The coordinator records the effort
// with the turn, so the item stays in the history at its place, and the
// prompt cache, keyed on the request's effort, survives every switch.
// After a compaction, which drops the updates it covers, the requests
// carry the effort the covered history last set instead (withEffortPin),
// as Codex sets a new baseline then. A model without updates, or effort
// ultra, which only the request can carry, gets the items stripped and
// the request's effort set as before.

// adaptiveRouter picks each turn request's effort.
type adaptiveRouter struct {
	// steps is how many levels a follow-up goes down: 1 or 2 (0: off).
	steps int
}

// effortChoice is a request's effort: the runner's level, ultra when the
// ultra client sends it, and why.
type effortChoice struct {
	effort llm.ReasoningEffort
	ultra  bool
	reason string
	// updated is set when a configuration update set the effort for the
	// request.
	updated bool
}

// effortUpdate is the coordinator's EffortUpdate: with effort updates in
// use, the effort to set for the turn request built as req, whose effort
// is the user's: adaptive effort's choice, else the user's, when the
// history last set another (lastEffort); "" otherwise. Respond logs the
// choice for its request.
func (s *switcher) effortUpdate(req llm.Request) llm.ReasoningEffort {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.update = nil
	if !s.updatingLocked(req.Model.ID) {
		return ""
	}
	c := effortChoice{effort: req.Model.ReasoningEffort}
	if s.adaptive.steps > 0 {
		c = s.adaptive.route(req.Input, c.effort, false)
	}
	// The first turn sets its effort too, as Codex's baseline.
	c.updated = c.effort != lastEffort(req.Input, s.base) || !slices.ContainsFunc(req.Input, isUpdate)
	s.update = &c
	if !c.updated {
		return ""
	}

	return c.effort
}

// updatingLocked reports whether a request to model, or to the live model
// when one is set, uses effort updates: effort_updates is on, the model
// takes them, and the effort is not ultra. It holds s.mu.
func (s *switcher) updatingLocked(model string) bool {
	return s.updates != nil && s.base != "" && !s.variant.ultra && s.updates(cmp.Or(s.model, model))
}

func isUpdate(it llm.Item) bool { return it.Type == llm.ItemConfigurationUpdate }

// effortPinKey carries the effort a compaction pins the request to.
type effortPinKey struct{}

// withEffortPin gives the request the effort to carry in place of the
// session's base: after a compaction, the one the covered history last set.
func withEffortPin(ctx context.Context, effort llm.ReasoningEffort) context.Context {
	return context.WithValue(ctx, effortPinKey{}, effort)
}

// requestEffort is the effort a request carries with effort updates: the
// compaction's pin, else base.
func requestEffort(ctx context.Context, base llm.ReasoningEffort) llm.ReasoningEffort {
	if pin, ok := ctx.Value(effortPinKey{}).(llm.ReasoningEffort); ok {
		return pin
	}

	return base
}

// lastEffort is the effort the input's last configuration update set, else
// base.
func lastEffort(input []llm.Item, base llm.ReasoningEffort) llm.ReasoningEffort {
	return llm.Request{Model: llm.Model{ReasoningEffort: base}, Input: input}.Effort()
}

// route picks the effort for a request at e (ultra: at effort ultra).
func (l adaptiveRouter) route(input []llm.Item, e llm.ReasoningEffort, ultra bool) effortChoice {
	tag := fmt.Sprintf("%d-steps: ", l.steps)
	if l.steps == 1 {
		tag = "1-step: "
	}
	if why := userTurn(input); why != "" {
		return effortChoice{effort: e, ultra: ultra, reason: tag + why}
	}

	return effortChoice{effort: lowerEffort(e, ultra, l.steps), reason: tag + "tool results only"}
}

// userTurn says why a request keeps the user's effort, or "" when its input
// since the model's last output is only tool results.
func userTurn(input []llm.Item) string {
	if !slices.ContainsFunc(input, modelOutput) {
		return "first request"
	}
	results := false
	for _, it := range slices.Backward(input) {
		if modelOutput(it) {
			break
		}
		if it.Type == llm.ItemToolResult {
			results = true
		} else if m, ok := it.Data.(llm.Message); ok && m.Role == llm.RoleUser {
			return "user message"
		}
	}
	if !results {
		return "no tool results"
	}

	return ""
}

// modelOutput reports whether an item is the model's: its text, a tool
// call, or reasoning.
func modelOutput(it llm.Item) bool {
	if m, ok := it.Data.(llm.Message); ok {
		return m.Role == llm.RoleAssistant
	}

	return it.Type == llm.ItemToolCall || it.Type == llm.ItemReasoning
}

// efforts are the runner's levels, lowest first.
var efforts = []llm.ReasoningEffort{llm.ReasoningEffortLow, llm.ReasoningEffortMedium, llm.ReasoningEffortHigh, llm.ReasoningEffortXHigh, llm.ReasoningEffortMax}

// lowerEffort is the effort steps levels below effort, never below low;
// ultra (the runner's max with the ultra client) is the level above max,
// so one step from it is max and two are xhigh.
func lowerEffort(effort llm.ReasoningEffort, ultra bool, steps int) llm.ReasoningEffort {
	i := slices.Index(efforts, effort)
	if ultra {
		i = len(efforts)
	}
	if i < 0 {
		return effort
	}

	return efforts[max(i-steps, 0)]
}

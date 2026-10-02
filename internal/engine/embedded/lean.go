package embedded

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/viktordanov/unreal-agent/harness/llm"

	"github.com/viktordanov/uah/internal/models"
)

// Lean mode's effort routing: every change is relative to the effort the
// user picked, E. A routine request goes the mode's steps lower, E-1 or
// E-2, never below low; the first request and every request that carries
// a user message go at E. The rules, picked for a benchmark with
// UAH_EXPERIMENTS=lean-rule=rN, decide which requests are routine:
//
//   - r0: a request whose input since the model's last output is only tool
//     results.
//   - r1 (the default): only when every one of those results is a plain
//     confirmation (leanclass.go); anything that brings content keeps E.
//   - r2: r1, and a request after the same command failed in each of the
//     last two turns goes at E+1, capped at the model's highest effort.
//   - r3: r2, and a reading-heavy session never goes below E: its first
//     message asks to review, investigate, explain, summarize, report,
//     audit, analyse, or why, or no edit succeeded in 4 model turns.
type leanRule int

const (
	leanR0 leanRule = iota
	leanR1
	leanR2
	leanR3
)

const defaultLeanRule = leanR1

func parseLeanRule(s string) (leanRule, bool) {
	switch s {
	case "r0":
		return leanR0, true
	case "r1":
		return leanR1, true
	case "r2":
		return leanR2, true
	case "r3":
		return leanR3, true
	}

	return 0, false
}

func (r leanRule) String() string { return fmt.Sprintf("r%d", int(r)) }

// readingTask matches a first message that asks for reading, not editing.
var readingTask = regexp.MustCompile(`(?i)\b(review|investigate|explain|summari[sz]e|report|audit|analy[sz]e|why)\b`)

// readingTurns is how many model turns without a successful edit make a
// session reading-heavy.
const readingTurns = 4

// leanRouter picks each turn request's effort.
type leanRouter struct {
	rule leanRule
	// steps is how many levels a routine request goes down: 1 or 2.
	steps int
	// top is the highest effort the model accepts, "" when unknown.
	top func(model string) llm.ReasoningEffort
}

func newLeanRouter(rule leanRule, steps int, catalog *models.Manager, provider string) *leanRouter {
	return &leanRouter{rule: rule, steps: steps, top: func(model string) llm.ReasoningEffort {
		if catalog == nil {
			return ""
		}
		md, ok := catalog.Cached(provider).Metadata(model)
		if !ok || len(md.ReasoningLevels) == 0 {
			return ""
		}

		return reasoningEffort(md.ReasoningLevels[len(md.ReasoningLevels)-1])
	}}
}

// tag starts each reason: the rule and the steps, as "r1, 2-steps: ".
func (l *leanRouter) tag() string {
	if l.steps == 1 {
		return l.rule.String() + ", 1-step: "
	}

	return fmt.Sprintf("%s, %d-steps: ", l.rule, l.steps)
}

// effortChoice is a request's effort: the runner's level, ultra when the
// ultra client sends it, and why.
type effortChoice struct {
	effort llm.ReasoningEffort
	ultra  bool
	reason string
}

// route picks the effort for a request at e (ultra: at effort ultra).
func (l *leanRouter) route(input []llm.Item, e llm.ReasoningEffort, ultra bool, model string) effortChoice {
	h := readHistory(input)
	tag := l.tag()
	keep := func(why string) effortChoice {
		return effortChoice{effort: e, ultra: ultra, reason: tag + why}
	}
	if len(h.turns) == 0 {
		return keep("first request")
	}
	last := h.turns[len(h.turns)-1]
	if last.user {
		return keep("user message")
	}
	if len(last.results) == 0 {
		return keep("no tool results")
	}
	lower := func(why string) effortChoice {
		return effortChoice{effort: lowerEffort(e, ultra, l.steps), reason: tag + why}
	}
	if l.rule == leanR0 {
		return lower("tool results only")
	}
	if l.rule >= leanR2 {
		if cmd := h.repeatedFailure(); cmd != "" {
			if up, ok := raiseEffort(e, ultra, l.top(model)); ok {
				return effortChoice{effort: up, reason: tag + "failed twice: " + cmd}
			}

			return keep("failed twice, at the highest effort: " + cmd)
		}
	}
	for _, r := range last.results {
		if r.kind != resultConfirm {
			return keep(r.signal)
		}
	}
	if l.rule >= leanR3 {
		if why := h.readingHeavy(); why != "" {
			return keep("reading-heavy: " + why)
		}
	}

	return lower("confirmations: " + last.signals())
}

// history is a request's input as turns: what each model output got back.
type history struct {
	turns     []leanTurn
	firstUser string
	edited    bool
}

// leanTurn is what came back after one model output: tool results, and
// whether a user message came too.
type leanTurn struct {
	results []classified
	user    bool
}

func (t leanTurn) signals() string {
	var s []string
	for _, r := range t.results {
		if !slices.Contains(s, r.signal) {
			s = append(s, r.signal)
		}
	}

	return strings.Join(s, ", ")
}

// readHistory splits the input into turns and classifies each result by
// the call it answers.
func readHistory(input []llm.Item) history {
	var h history
	calls := map[string]llm.ToolCall{}
	output := false
	for _, it := range input {
		switch d := it.Data.(type) {
		case llm.ToolCall:
			calls[d.CallID] = d
			h.startTurn(&output)
		case llm.ToolResult:
			output = false
			if len(h.turns) > 0 {
				call, ok := calls[d.CallID]
				c := classify(call, ok, d)
				h.edited = h.edited || c.edit
				last := &h.turns[len(h.turns)-1]
				last.results = append(last.results, c)
			}
		case llm.Message:
			switch d.Role {
			case llm.RoleAssistant:
				h.startTurn(&output)
			case llm.RoleUser:
				output = false
				if len(h.turns) > 0 {
					h.turns[len(h.turns)-1].user = true
				} else if h.firstUser == "" && !strings.HasPrefix(d.Text, primedOpen) {
					h.firstUser = d.Text
				}
			case llm.RoleSystem:
			}
		default:
			if it.Type == llm.ItemReasoning {
				h.startTurn(&output)
			}
		}
	}

	return h
}

// startTurn starts a turn at the first item of a model output.
func (h *history) startTurn(output *bool) {
	if !*output {
		h.turns = append(h.turns, leanTurn{})
		*output = true
	}
}

// repeatedFailure is a command, as normalized, that failed in each of the
// last two turns, or "".
func (h history) repeatedFailure() string {
	n := len(h.turns)
	if n < 2 {
		return ""
	}
	for _, r := range h.turns[n-1].results {
		if r.kind != resultFailure || r.command == "" {
			continue
		}
		for _, p := range h.turns[n-2].results {
			if p.kind == resultFailure && p.command == r.command {
				return r.command
			}
		}
	}

	return ""
}

// readingHeavy says why the session is reading-heavy, or "".
func (h history) readingHeavy() string {
	if m := readingTask.FindString(h.firstUser); m != "" {
		return "the task says " + strings.ToLower(m)
	}
	if len(h.turns) >= readingTurns && !h.edited {
		return fmt.Sprintf("no edit in %d turns", len(h.turns))
	}

	return ""
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

// raiseEffort is the effort one level above effort, unless that passes
// top (the model's highest; "" is the runner's max) or effort is ultra.
func raiseEffort(effort llm.ReasoningEffort, ultra bool, top llm.ReasoningEffort) (llm.ReasoningEffort, bool) {
	i := slices.Index(efforts, effort)
	if ultra || i < 0 || i+1 >= len(efforts) {
		return effort, false
	}
	if t := slices.Index(efforts, top); t >= 0 && i+1 > t {
		return effort, false
	}

	return efforts[i+1], true
}

// Package eval measures what a compaction strategy does to one model
// request: the tokens it frees per actor, the headroom it leaves, the
// prompt cache it breaks, the facts it keeps (by rules, no model), the
// user messages and skill bodies it keeps, and how likely the next calls
// are to fetch again what it dropped. It is pure: requests in, numbers
// out. The runner that captures real requests from recorded sessions is
// internal/compaction/evalrun. See docs/design/compaction.md.
package eval

import (
	"path"
	"strings"

	"github.com/viktordanov/uah-core/harness/llm"

	"github.com/viktordanov/uah/internal/compaction"
)

// Actor is who a part of a request comes from.
type Actor string

const (
	ActorSystem     Actor = "system"
	ActorUser       Actor = "user"
	ActorSummary    Actor = "summary"
	ActorAssistant  Actor = "assistant"
	ActorReasoning  Actor = "reasoning"
	ActorToolCall   Actor = "tool_call"
	ActorToolOutput Actor = "tool_output"
)

// Actors are every actor, in the order tables list them.
var Actors = []Actor{ActorSystem, ActorUser, ActorSummary, ActorAssistant, ActorReasoning, ActorToolCall, ActorToolOutput}

// ActorOf is the item's actor. A summary message, and a note that carries
// a covered call's output, count as the summary.
func ActorOf(item llm.Item) Actor {
	switch d := item.Data.(type) {
	case llm.Message:
		switch {
		case d.Role == llm.RoleSystem:
			return ActorSystem
		case d.Role == llm.RoleAssistant:
			return ActorAssistant
		case strings.HasPrefix(d.Text, compaction.SummaryPrefix):
			return ActorSummary
		}

		return ActorUser
	case llm.ToolCall:
		return ActorToolCall
	case llm.ToolResult:
		return ActorToolOutput
	case llm.Reasoning:
		return ActorReasoning
	}

	return ActorUser
}

// Compose is the estimated tokens per actor.
func Compose(items []llm.Item) map[Actor]int64 {
	out := map[Actor]int64{}
	for _, item := range items {
		out[ActorOf(item)] += compaction.EstimateTokens([]llm.Item{item})
	}

	return out
}

// Case is one point in a recorded session: the request the context builder
// produced there, and the tool calls the session made after it.
type Case struct {
	Name   string
	Before []llm.Item
	Later  []llm.ToolCall
	// Focus is a /compact focus, when the case has one.
	Focus string
	// Window is the model's context window, for the headroom.
	Window int64
}

// Result is one strategy's effect on one case.
type Result struct {
	Before, After int64
	// ByActor are the tokens per actor after, and ByActorBefore before.
	ByActor, ByActorBefore map[Actor]int64
	// Headroom is the window left after.
	Headroom int64
	// CacheMiss is the tokens after the longest prefix the request after
	// shares with the one before: what the first request after it pays
	// uncached.
	CacheMiss int64
	// Recall counts the facts of the covered history found in the request
	// after, per kind.
	Recall map[Kind]Count
	// UserKept is the share of user message tokens kept word for word;
	// SkillsKept the share of SkillUse bodies still there.
	UserKept, SkillsKept Count
	// Refetch counts the later calls that name a path read before whose
	// output is gone, among the later calls (the next MaxLater).
	Refetch Count
	// CallInput is the input of the model call the strategy made, such as
	// the summary call (0: none). Most of it hits the prompt cache.
	CallInput int64
}

// Count is found out of total.
type Count struct{ Found, Total int }

// Add sums counts.
func (c Count) Add(o Count) Count { return Count{c.Found + o.Found, c.Total + o.Total} }

// Share is Found/Total, or -1 when Total is 0.
func (c Count) Share() float64 {
	if c.Total == 0 {
		return -1
	}

	return float64(c.Found) / float64(c.Total)
}

// MaxLater is how many later calls the re-fetch count looks at, as the
// compaction research did.
const MaxLater = 20

// Measure compares the request after a strategy with the case's request
// before it.
func Measure(c Case, after []llm.Item) Result {
	r := Result{
		Before: compaction.EstimateTokens(c.Before), After: compaction.EstimateTokens(after),
		ByActor: Compose(after), ByActorBefore: Compose(c.Before),
	}
	if c.Window > 0 {
		r.Headroom = c.Window - r.After
	}
	r.CacheMiss = r.After - compaction.EstimateTokens(after[:commonPrefix(c.Before, after)])
	covered := c.Before[min(1, len(c.Before)) : 1+compaction.Coverable(c.Before)]
	text := visibleText(after[min(1, len(after)):]) // the system prompt keeps what it always has
	r.Recall = recall(compaction.ExtractFacts(covered), c.Focus, text)
	r.UserKept = userKept(covered, after)
	r.SkillsKept = skillsKept(covered, after)
	r.Refetch = refetch(covered, after, c.Later)

	return r
}

// commonPrefix is how many items a and b share from the start.
func commonPrefix(a, b []llm.Item) int {
	n := 0
	for n < len(a) && n < len(b) && same(a[n], b[n]) {
		n++
	}

	return n
}

func same(a, b llm.Item) bool {
	return compaction.EstimateTokens([]llm.Item{a}) == compaction.EstimateTokens([]llm.Item{b}) && itemText(a) == itemText(b)
}

// itemText is what the model reads of an item.
func itemText(item llm.Item) string {
	switch d := item.Data.(type) {
	case llm.Message:
		return d.Text
	case llm.ToolCall:
		return d.Name + " " + d.Arguments
	case llm.ToolResult:
		return compaction.ResultText(d)
	case llm.Reasoning:
		return strings.Join(d.Summary, "\n") + string(d.Raw)
	}

	return ""
}

// visibleText is all the text of a request, for fact checks.
func visibleText(items []llm.Item) string {
	var b strings.Builder
	for _, item := range items {
		b.WriteString(itemText(item))
		b.WriteByte('\n')
	}

	return b.String()
}

// present is the research's rule: the fact, or its base name, appears.
func present(fact, text string) bool {
	base := path.Base(strings.TrimRight(fact, "/"))

	return strings.Contains(text, fact) || (base != "." && base != "/" && strings.Contains(text, base))
}

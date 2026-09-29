package evalrun

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/llm"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/compaction/eval"
)

// Strategy rewrites a case's request as one way of compacting would.
type Strategy struct {
	Name string
	// Opaque is a strategy whose summary the rules cannot read.
	Opaque bool
	Apply  apply
}

// apply is a strategy's rewrite of a case: the request after, and the
// input of the model call it made (0: none).
type apply func(ctx context.Context, c eval.Case, s *Summaries) ([]llm.Item, int64, error)

// Strategies are the strategies the evaluation compares: the full history,
// Codex's local summary, uah's additions one by one, elision alone, what
// uah's automatic compaction does, and Codex's remote shape.
func Strategies() []Strategy {
	return []Strategy{
		{Name: "none", Apply: func(_ context.Context, c eval.Case, _ *Summaries) ([]llm.Item, int64, error) { return c.Before, 0, nil }},
		{Name: "codex (summary)", Apply: summarized(0, false)},
		{Name: "summary + ledger", Apply: summarized(0, true)},
		{Name: "summary + ledger + last 5 calls", Apply: summarized(5, true)},
		{Name: "summary + ledger + last 10 calls", Apply: summarized(10, true)},
		{Name: "elide (10 calls)", Apply: elided(compaction.Elision{AfterCalls: 10})},
		{Name: "elide (10 calls, big after 3)", Apply: elided(compaction.DefaultElision)},
		{Name: "uah automatic", Apply: automatic(compaction.DefaultElision, compaction.DefaultKeepCalls)},
		{Name: "codex remote (item ≈ summary)", Opaque: true, Apply: remote},
	}
}

// Only are the strategies whose names are in names (all when empty).
func Only(all []Strategy, names []string) []Strategy {
	if len(names) == 0 {
		return all
	}
	var out []Strategy
	for _, s := range all {
		if slices.Contains(names, s.Name) {
			out = append(out, s)
		}
	}

	return out
}

// summarized covers all but the last keep calls with the summary, and adds
// the ledger when asked, as the engine's compactor does.
func summarized(keep int, ledger bool) apply {
	return func(ctx context.Context, c eval.Case, s *Summaries) ([]llm.Item, int64, error) {
		rec, ok, err := summaryRecord(ctx, c, s, keep)
		if err != nil || !ok {
			return c.Before, 0, err
		}
		if ledger {
			rec.Ledger = compaction.Ledger(c.Before[1:1+rec.Covered], c.Focus)
		}
		out, err := compaction.Apply(c.Before, rec)

		return out, callInput(c, rec), err //nolint:wrapcheck // Apply's errors say what failed
	}
}

// callInput is what the summary call sends: the covered history and the
// prompt.
func callInput(c eval.Case, rec compaction.Record) int64 {
	return compaction.EstimateTokens(c.Before[:1+rec.Covered]) + int64(compaction.ApproxTokens(compaction.Prompt))
}

func summaryRecord(ctx context.Context, c eval.Case, s *Summaries, keep int) (compaction.Record, bool, error) {
	covered := compaction.CoverableKeeping(c.Before, keep)
	if covered == 0 {
		return compaction.Record{}, false, nil
	}
	hash, err := compaction.Hash(c.Before[1 : 1+covered])
	if err != nil {
		return compaction.Record{}, false, err
	}
	summary, err := s.Get(ctx, c.Before[:1+covered], hash)
	if err != nil {
		return compaction.Record{}, false, err
	}
	rec, err := compaction.NewRecordCovering(c.Before, covered, summary, compaction.TriggerAuto, "", time.Time{})
	rec.Keep, rec.Focus = compaction.Settings{}.KeepFor(c.Window), c.Focus

	return rec, err == nil, err
}

func elided(rules compaction.Elision) apply {
	return func(_ context.Context, c eval.Case, _ *Summaries) ([]llm.Item, int64, error) {
		out, err := compaction.Apply(c.Before, compaction.Record{}.WithElided(rules.Elidable(c.Before, nil)))

		return out, 0, err //nolint:wrapcheck // as above
	}
}

// automatic is the engine's automatic compaction, as if the case's request
// had just reached the limit: the stubs alone when they bring it under
// three quarters of that, else the summary and ledger.
func automatic(rules compaction.Elision, keep int) apply {
	return func(ctx context.Context, c eval.Case, s *Summaries) ([]llm.Item, int64, error) {
		out, _, err := elided(rules)(ctx, c, s)
		if err == nil && compaction.EstimateTokens(out) <= compaction.EstimateTokens(c.Before)*3/4 {
			return out, 0, nil
		}

		return summarized(keep, true)(ctx, c, s)
	}
}

// remoteKeepTokens is Codex's cap on the messages remote compaction keeps
// (RETAINED_MESSAGE_TOKEN_BUDGET, rust-v0.159.1).
const remoteKeepTokens = 64_000

// remote is Codex's remote v2 shape: the user messages up to 64,000
// tokens, then the provider's encrypted item. Offline the item's size is
// taken as the summary's; its facts cannot be read.
func remote(ctx context.Context, c eval.Case, s *Summaries) ([]llm.Item, int64, error) {
	rec, ok, err := summaryRecord(ctx, c, s, 0)
	if err != nil || !ok {
		return c.Before, 0, err
	}
	rec.Keep = min(remoteKeepTokens, max(int(c.Window/4), 1))
	rec.Summary = opaque(compaction.ApproxTokens(rec.Summary))
	out, err := compaction.Apply(c.Before, rec)

	return out, callInput(c, rec), err //nolint:wrapcheck // as above
}

// opaque is text of about tokens tokens that no fact matches.
func opaque(tokens int) string {
	b := make([]byte, 0, tokens*4)
	for len(b) < tokens*4 {
		b = fmt.Appendf(b, "%04x", len(b))
	}

	return string(b)
}

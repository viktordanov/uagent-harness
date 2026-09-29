package compaction

import (
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/llm"
)

// Strategy names how a compaction shrank the context.
type Strategy string

const (
	// StrategyLocal is a summary the session's provider wrote as text
	// (Codex's local compaction).
	StrategyLocal Strategy = "local"
	// StrategyRemote is the provider's encrypted compaction item (Codex's
	// remote compaction v2).
	StrategyRemote Strategy = "remote"
	// StrategyElide replaced old tool outputs with stubs, with no summary.
	StrategyElide Strategy = "elide"
	// StrategyClear is /clear: everything dropped, no summary.
	StrategyClear Strategy = "clear"
)

// Phase says where in a turn a compaction ran: before the turn's first
// model request (the new message waits for it), or between the requests
// of a turn, after tool outputs.
type Phase string

const (
	PhasePreTurn Phase = "pre-turn"
	PhaseMidTurn Phase = "mid-turn"
)

// PhaseOf is the phase of a compaction before the request input: pre-turn
// when the input ends with new user messages, else mid-turn.
func PhaseOf(input []llm.Item) Phase {
	if n := len(input); n > 1 && IsUserMessage(input[n-1]) {
		return PhasePreTurn
	}

	return PhaseMidTurn
}

// Usage is what the summary call used, in tokens.
type Usage struct {
	Input     int64 `json:"input"`
	Cached    int64 `json:"cached,omitempty"`
	Output    int64 `json:"output"`
	Reasoning int64 `json:"reasoning,omitempty"`
}

// UsageOf converts the runner's usage.
func UsageOf(u llm.Usage) Usage {
	return Usage{Input: u.InputTokens, Cached: u.CachedInputTokens, Output: u.OutputTokens, Reasoning: u.ReasoningTokens}
}

// Stats measure one compaction, so compactions can be compared across
// sessions and strategies. Token counts before and after are the context
// in use (Codex's measure, InUse) and the estimate of the rewritten
// request.
type Stats struct {
	Strategy Strategy `json:"strategy"`
	Phase    Phase    `json:"phase,omitempty"`
	Before   int64    `json:"tokens_before"`
	After    int64    `json:"tokens_after"`
	// SummaryTokens is the summary's estimate (for a remote compaction,
	// the call's output tokens); LedgerTokens the state ledger's.
	SummaryTokens int64 `json:"summary_tokens,omitempty"`
	LedgerTokens  int64 `json:"ledger_tokens,omitempty"`
	// Elided is how many tool outputs are stubs after the compaction.
	Elided int `json:"elided,omitempty"`
	// Call is the summary call's usage; zero when none ran.
	Call     Usage `json:"call,omitzero"`
	Duration int64 `json:"duration_ms"`
}

// Took is the duration.
func (s Stats) Took() time.Duration { return time.Duration(s.Duration) * time.Millisecond }

// Line is the stats in one line, for progress output and transcripts:
// "145,809 → 4,614 tokens (local, pre-turn, 521-token summary, 12.3s)".
func (s Stats) Line() string {
	detail := string(s.Strategy)
	if s.Phase != "" {
		detail += ", " + string(s.Phase)
	}
	if s.SummaryTokens > 0 {
		detail += fmt.Sprintf(", %s-token summary", Commas(s.SummaryTokens))
	}
	if s.LedgerTokens > 0 {
		detail += fmt.Sprintf(", %s-token ledger", Commas(s.LedgerTokens))
	}
	if s.Elided > 0 {
		detail += fmt.Sprintf(", %d outputs elided", s.Elided)
	}
	if s.Took() >= 100*time.Millisecond {
		detail += ", " + s.Took().Round(100*time.Millisecond).String()
	}

	return fmt.Sprintf("%s → %s tokens (%s)", Commas(s.Before), Commas(s.After), detail)
}

// Attrs are the stats as log attributes.
func (s Stats) Attrs() []slog.Attr {
	return []slog.Attr{
		slog.String("strategy", string(s.Strategy)),
		slog.String("phase", string(s.Phase)),
		slog.Int64("tokens_before", s.Before),
		slog.Int64("tokens_after", s.After),
		slog.Int64("summary_tokens", s.SummaryTokens),
		slog.Int64("ledger_tokens", s.LedgerTokens),
		slog.Int("elided", s.Elided),
		slog.Int64("call_input", s.Call.Input),
		slog.Int64("call_cached", s.Call.Cached),
		slog.Int64("call_output", s.Call.Output),
		slog.Duration("duration", s.Took()),
	}
}

// Commas formats n with thousands separators.
func Commas(n int64) string {
	s := strconv.FormatInt(n, 10)
	if n < 0 {
		return "-" + Commas(-n)
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}

	return s
}

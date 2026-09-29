package embedded

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/llm"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/engine"
)

// run summarizes the history as the model would see it, records the
// compaction, and reports it. On failure the request goes out uncompacted.
func (c *compactor) run(ctx context.Context, job *compactionJob, req llm.Request, opts llm.RequestOptions, ask compactionAsk) {
	defer close(job.done)
	defer job.cancel()
	trigger := ask.trigger
	c.emit(engine.CompactionStarted{At: time.Now(), Trigger: trigger, Tokens: ask.used})
	rec, err := c.compact(ctx, req, opts, ask)
	interrupted := err != nil && ctx.Err() != nil
	job.interrupted = interrupted
	if err != nil {
		c.mu.Lock()
		c.job = nil
		if trigger == compaction.TriggerAuto && !interrupted {
			c.autoFailures++
		}
		c.mu.Unlock()
		if interrupted {
			err = fmt.Errorf("interrupted: %w", err)
		}
		c.emit(engine.Compacted{At: time.Now(), Trigger: trigger, Err: err.Error(), Interrupted: interrupted})

		return
	}
	warning := c.stillFull(req.Input, rec, trigger)
	c.mu.Lock()
	c.job = nil
	c.record, c.stale, c.used = &rec, false, 0
	c.autoFailures = 0
	if warning != "" {
		// Another compaction cannot shrink what stays: the system prompt,
		// the kept user messages, and a summary.
		c.autoFailures = maxAutoFailures
	}
	c.mu.Unlock()
	if c.logger != nil && rec.Stats != nil {
		c.logger.LogAttrs(ctx, slog.LevelInfo, "compacted the context",
			append([]slog.Attr{slog.String("trigger", string(trigger))}, rec.Stats.Attrs()...)...)
	}
	c.emit(engine.Compacted{At: time.Now(), Trigger: trigger, Summary: rec.Summary, Warning: warning, Stats: rec.Stats})
}

// stillFull warns when an automatic compaction left the context at or
// above the automatic limit: without the warning, and the stop that goes
// with it, every later request would compact again.
func (c *compactor) stillFull(input []llm.Item, rec compaction.Record, trigger compaction.Trigger) string {
	limit := c.autoLimit()
	if trigger != compaction.TriggerAuto || limit == 0 {
		return ""
	}
	view, err := compaction.Apply(input, rec)
	if err != nil || compaction.EstimateTokens(view) < limit {
		return ""
	}

	return fmt.Sprintf("the context is still about %d tokens after compacting, above the automatic limit of %d; "+
		"automatic compaction stops for this run (/compact still works)", compaction.EstimateTokens(view), limit)
}

func (c *compactor) compact(ctx context.Context, req llm.Request, opts llm.RequestOptions, ask compactionAsk) (compaction.Record, error) {
	start := time.Now()
	if ask.trigger == compaction.TriggerClear {
		return c.clear(req.Input, ask, start)
	}
	if c.before != nil {
		if err := c.before(ctx, ask.trigger); err != nil {
			return compaction.Record{}, fmt.Errorf("compaction was stopped: %w", err)
		}
	}
	covered := compaction.Coverable(req.Input)
	if covered == 0 {
		return compaction.Record{}, errNothingToCompact
	}
	summarize := c.summarize
	if summarize == nil {
		summarize = c.localSummary(req.Model.ReasoningEffort, opts.CacheKey, ask.focus)
	}
	summary, err := summarize(ctx, c.apply(req.Input[:1+covered]))
	if err != nil {
		return compaction.Record{}, err
	}
	rec, err := compaction.NewRecord(req.Input, summary.Text, ask.trigger, c.summaryModel(), time.Now().UTC())
	if err != nil {
		return compaction.Record{}, err
	}
	window := compaction.ContextWindow(c.next.currentModel(), c.window, c.windows)
	rec.Keep, rec.Focus = c.settings.KeepFor(window), strings.TrimSpace(ask.focus)
	// What a /clear dropped stays dropped.
	c.mu.Lock()
	if c.record != nil && !c.stale {
		rec.Floor = min(c.record.Floor, rec.Covered)
	}
	c.mu.Unlock()
	rec.Ledger = compaction.Ledger(req.Input[1+rec.Floor:1+rec.Covered], rec.Focus)
	stats := c.measure(req.Input, rec, ask, start, compaction.StrategyLocal)
	stats.SummaryTokens, stats.Call = int64(compaction.ApproxTokens(summary.Text)), compaction.UsageOf(summary.Usage)
	stats.LedgerTokens = int64(compaction.ApproxTokens(rec.Ledger))
	rec.Stats = &stats
	if err := c.log.Append(rec); err != nil {
		return compaction.Record{}, err
	}

	return rec, nil
}

// measure is a compaction's stats before its summary's: the context in use
// before, the estimate of the request after, the phase, and the time since
// start.
func (c *compactor) measure(input []llm.Item, rec compaction.Record, ask compactionAsk, start time.Time, strategy compaction.Strategy) compaction.Stats {
	stats := compaction.Stats{Strategy: strategy, Phase: compaction.PhaseOf(input), Before: ask.used, Duration: time.Since(start).Milliseconds()}
	if view, err := compaction.Apply(input, rec); err == nil {
		stats.After = compaction.EstimateTokens(view)
	}

	return stats
}

// clear records a /clear: every item so far is dropped from what the model
// sees, with no summary and no model call. The session file keeps them.
func (c *compactor) clear(input []llm.Item, ask compactionAsk, start time.Time) (compaction.Record, error) {
	if compaction.Coverable(input) == 0 {
		return compaction.Record{}, errNothingToCompact
	}
	rec, err := compaction.NewClear(input, time.Now().UTC())
	if err != nil {
		return compaction.Record{}, err
	}
	stats := c.measure(input, rec, ask, start, compaction.StrategyClear)
	rec.Stats = &stats
	if err := c.log.Append(rec); err != nil {
		return compaction.Record{}, err
	}

	return rec, nil
}

// summaryModel is the model that writes the summary: compact_model, or the
// session's current model, as Codex's local compaction uses.
func (c *compactor) summaryModel() string {
	if c.settings.Model != "" {
		return c.settings.Model
	}

	return c.next.currentModel()
}

// localSummary asks the summary model with the configured prompt and the
// user's focus. The effort is compact_effort, else the session's; the
// window is the summary model's.
func (c *compactor) localSummary(effort llm.ReasoningEffort, cacheKey, focus string) compaction.Summarizer {
	if c.settings.Effort != "" {
		effort = c.settings.Effort
	}
	window := compaction.ContextWindow(c.next.currentModel(), c.window, c.windows)
	if c.settings.Model != "" && c.settings.Model != c.next.currentModel() {
		window = compaction.ContextWindow(c.settings.Model, 0, c.windows)
	}

	return func(ctx context.Context, view []llm.Item) (compaction.Summary, error) {
		return compaction.Summarize(ctx, compaction.SummaryCall{ //nolint:wrapcheck // Summarize wraps its errors
			Adapter: c.next.direct(), Model: c.settings.Model, Effort: effort, CacheKey: cacheKey,
			Window: window, Prompt: c.settings.SummaryPrompt(focus),
		}, view)
	}
}

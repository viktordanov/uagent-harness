package embedded

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/llm"

	"github.com/viktordanov/uah/internal/compaction"
)

// remoteCompact asks the provider for Codex's remote compaction: the turn
// request's history as the model sees it, its tools and model, and the
// trigger the transport appends (remotecompact.go). The record keeps the
// user messages up to Codex's 64,000 tokens, then the item, then uah's
// ledger. The covered range is Codex's: everything but the new messages.
func (c *compactor) remoteCompact(ctx context.Context, req llm.Request, opts llm.RequestOptions, ask compactionAsk, start time.Time) (compaction.Record, error) {
	covered := compaction.Coverable(req.Input)
	if covered == 0 {
		return compaction.Record{}, errNothingToCompact
	}
	call := &remoteCall{}
	callCtx := c.withItem(context.WithValue(ctx, remoteCallKey{}, call))
	resp, err := c.next.Respond(callCtx, llm.Request{Model: req.Model, Input: c.apply(req.Input[:1+covered]), Tools: req.Tools}, opts)
	if err != nil {
		return compaction.Record{}, err //nolint:wrapcheck // the coordinator's model errors read as they are
	}
	item, err := call.result()
	if err != nil {
		return compaction.Record{}, err
	}
	rec, err := compaction.NewRecordCovering(req.Input, covered, "", ask.trigger, c.next.currentModel(), time.Now().UTC())
	if err != nil {
		return compaction.Record{}, err
	}
	window := compaction.ContextWindow(c.next.currentModel(), c.window, c.windows)
	rec.Remote, rec.Keep = item, c.settings.RemoteKeepFor(window)
	c.carry(&rec, req.Input)
	rec.Ledger = compaction.Ledger(req.Input[1+rec.Floor:1+rec.Covered], "")
	stats := c.measure(req.Input, rec, ask, start, compaction.StrategyRemote)
	// The placeholder stands for the item, about as large as the call's output.
	stats.After += resp.Usage.OutputTokens - int64(compaction.ApproxTokens(compaction.RemotePlaceholder(rec)))
	stats.SummaryTokens, stats.Call = resp.Usage.OutputTokens, compaction.UsageOf(resp.Usage)
	stats.LedgerTokens, stats.Elided = int64(compaction.ApproxTokens(rec.Ledger)), len(rec.Elided)
	rec.Stats = &stats
	if err := c.log.Append(rec); err != nil {
		return compaction.Record{}, err
	}

	return rec, nil
}

// usesRemote reports whether a compaction goes to the provider: when the
// provider can and remote_compaction is on, except a /compact with a focus,
// which only a summary prompt can take.
func (c *compactor) usesRemote(ask compactionAsk) bool {
	return c.remote && c.summarize == nil && strings.TrimSpace(ask.focus) == ""
}

// tryRemote runs a remote compaction; ok is false when it failed and the
// local summary should run instead, as Codex falls back.
func (c *compactor) tryRemote(ctx context.Context, req llm.Request, opts llm.RequestOptions, ask compactionAsk, start time.Time) (compaction.Record, bool, error) {
	rec, err := c.remoteCompact(ctx, req, opts, ask, start)
	switch {
	case err == nil:
		return rec, true, nil
	case ctx.Err() != nil || err == errNothingToCompact: //nolint:errorlint // a sentinel of this package
		return compaction.Record{}, true, err
	}
	if c.logger != nil {
		c.logger.LogAttrs(ctx, slog.LevelWarn, "remote compaction failed; summarizing locally", slog.String("error", err.Error()))
	}

	return compaction.Record{}, false, nil
}

// withItem gives a request the item of the latest compaction, when it is a
// remote one, for the transport to put in place of its placeholder.
func (c *compactor) withItem(ctx context.Context) context.Context {
	c.mu.Lock()
	rec := c.record
	stale := c.stale
	c.mu.Unlock()
	if stale {
		return ctx
	}

	return withRemoteItem(ctx, rec)
}

// carry brings forward what the latest compaction keeps: the floor a
// /clear set, and the elided outputs still after the covered items.
func (c *compactor) carry(rec *compaction.Record, input []llm.Item) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.record != nil && !c.stale {
		rec.Floor = min(c.record.Floor, rec.Covered)
		rec.Elided = compaction.StillElided(input[1+rec.Covered:], c.record.Elided)
	}
}

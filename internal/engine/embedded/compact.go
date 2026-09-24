package embedded

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/llm"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/llmcall"
)

var errNothingToCompact = errors.New("there is nothing to compact yet")

// compactor is the llm.Adapter the coordinator calls, in front of the
// switcher. It rewrites every request with the session's latest compaction,
// and compacts first when /compact asked for it or the context in use reached
// the automatic limit. The runner's context builder keeps the full history;
// only what goes to the model changes.
type compactor struct {
	next *switcher
	log  compactionLog
	emit func(core.Event)
	// before runs as each compaction starts; an error cancels it. It is where
	// a PreCompact hook attaches.
	before  func(context.Context, compaction.Trigger) error
	window  int64 // the configured window; 0 uses the model table
	percent int   // automatic compaction limit; 0 turns it off

	mu       sync.Mutex
	record   *compaction.Record
	pending  bool
	used     int64
	reported bool // a mismatch was reported
}

// requestCompaction compacts before the next model request.
func (c *compactor) requestCompaction() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending = true
}

func (c *compactor) Respond(ctx context.Context, req llm.Request, opts llm.RequestOptions) (llm.Response, error) {
	if trigger, ok := c.due(req.Input); ok {
		c.compact(ctx, req, opts, trigger)
	}
	req.Input = c.apply(req.Input)
	resp, err := c.next.Respond(ctx, req, opts)
	if err == nil {
		c.mu.Lock()
		c.used = resp.Usage.InputTokens + resp.Usage.OutputTokens
		c.mu.Unlock()
	}

	return resp, err
}

// due reports whether to compact before this request, and why. Automatic
// compaction needs something new to summarize, so a history that stays
// large after compacting does not compact on every request.
func (c *compactor) due(input []llm.Item) (compaction.Trigger, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending {
		return compaction.TriggerManual, true
	}
	limit := compaction.AutoLimit(compaction.ContextWindow(c.next.currentModel(), c.window), c.percent)
	if limit == 0 || c.used < limit {
		return "", false
	}
	covered := 0
	if c.record != nil {
		covered = c.record.Covered
	}
	if compaction.Coverable(input) > covered {
		return compaction.TriggerAuto, true
	}

	return "", false
}

// compact summarizes the history as the model would see it, records the
// compaction, and reports it. On failure the request goes out uncompacted.
func (c *compactor) compact(ctx context.Context, req llm.Request, opts llm.RequestOptions, trigger compaction.Trigger) {
	c.mu.Lock()
	c.pending = false
	used := c.used
	c.mu.Unlock()
	c.emit(engine.CompactionStarted{At: time.Now(), Trigger: trigger, Tokens: used})
	rec, err := c.summarize(ctx, req, opts, trigger)
	if err != nil {
		c.emit(engine.Compacted{At: time.Now(), Trigger: trigger, Err: err.Error()})

		return
	}
	c.mu.Lock()
	c.record, c.used, c.reported = &rec, 0, false
	c.mu.Unlock()
	c.emit(engine.Compacted{At: time.Now(), Trigger: trigger, Summary: rec.Summary})
}

func (c *compactor) summarize(ctx context.Context, req llm.Request, opts llm.RequestOptions, trigger compaction.Trigger) (compaction.Record, error) {
	if c.before != nil {
		if err := c.before(ctx, trigger); err != nil {
			return compaction.Record{}, fmt.Errorf("compaction was stopped: %w", err)
		}
	}
	covered := compaction.Coverable(req.Input)
	if covered == 0 {
		return compaction.Record{}, errNothingToCompact
	}
	system, input := compaction.SummaryRequest(c.apply(req.Input[:1+covered]))
	res, err := llmcall.Call(ctx, c.next.direct(), llmcall.Request{
		Effort: req.Model.ReasoningEffort, Instructions: system, Input: input, CacheKey: opts.CacheKey,
	})
	if err != nil {
		return compaction.Record{}, fmt.Errorf("failed to summarize the context: %w", err)
	}
	rec, err := compaction.NewRecord(req.Input, res.Text, trigger, c.next.currentModel(), time.Now().UTC())
	if err != nil {
		return compaction.Record{}, err
	}
	if err := c.log.append(rec); err != nil {
		return compaction.Record{}, err
	}

	return rec, nil
}

// apply rewrites the input with the latest compaction. A history that does
// not match it (a session file changed outside uah) goes out in full, and
// the mismatch is reported once.
func (c *compactor) apply(input []llm.Item) []llm.Item {
	c.mu.Lock()
	rec := c.record
	c.mu.Unlock()
	if rec == nil {
		return input
	}
	out, err := compaction.Apply(input, *rec)
	if err == nil {
		return out
	}
	c.mu.Lock()
	report := !c.reported
	c.reported = true
	c.mu.Unlock()
	if report {
		msg := "the saved compaction no longer matches the session; sending the full history"
		if !errors.Is(err, compaction.ErrMismatch) {
			msg = err.Error()
		}
		c.emit(engine.Compacted{At: time.Now(), Trigger: rec.Trigger, Err: msg})
	}

	return input
}

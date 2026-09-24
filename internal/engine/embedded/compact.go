package embedded

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/session"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/engine"
)

var errNothingToCompact = errors.New("there is nothing to compact yet")

// maxAutoFailures stops automatic compaction for the rest of a run after
// this many failed attempts in a row, so a failing summary call is not
// repeated before every model request.
const maxAutoFailures = 3

// compactor is the llm.Adapter the coordinator calls, in front of the
// switcher. It rewrites every request with the session's latest compaction,
// and compacts first when /compact asked for it or the context in use
// reached the automatic limit. The runner's context builder keeps the full
// history; only what goes to the model changes.
//
// A compaction runs as a job under the run's context, not the request's:
// the coordinator cancels a model request when a message arrives, and the
// next request waits for the same job instead of starting over. An
// interrupt or the run's end cancels the job.
type compactor struct {
	ctx  context.Context
	next *switcher
	log  compaction.Log
	emit func(core.Event)
	// summarize is the summary call; compaction.Summarize over the
	// switcher's current client when nil. A remote strategy plugs in here.
	summarize compaction.Summarizer
	// before runs as each compaction starts; an error cancels it. It is where
	// a PreCompact hook attaches.
	before  func(context.Context, compaction.Trigger) error
	window  int64 // the configured window; 0 uses the model table
	percent int   // automatic compaction limit; 0 turns it off

	mu      sync.Mutex
	record  *compaction.Record
	stale   bool // the record does not match the history; reported once
	pending bool
	used    int64 // the last response's total tokens; 0 when unknown
	job     *compactionJob
	// autoFailures counts failed automatic compactions in a row.
	autoFailures int
}

type compactionJob struct {
	done   chan struct{}
	cancel context.CancelFunc
	// interrupted is set before done closes: the request that waited must
	// not go out, since the run is stopping.
	interrupted bool
}

// requestCompaction compacts before the next model request.
func (c *compactor) requestCompaction() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending = true
}

// interrupt cancels a compaction in progress.
func (c *compactor) interrupt() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.job != nil {
		c.job.cancel()
	}
}

// stop cancels a compaction in progress and waits for it, so none outlives
// the run.
func (c *compactor) stop() error {
	c.mu.Lock()
	job := c.job
	c.mu.Unlock()
	if job != nil {
		job.cancel()
		<-job.done
	}

	return nil
}

func (c *compactor) Respond(ctx context.Context, req llm.Request, opts llm.RequestOptions) (llm.Response, error) {
	// The job runs under the run's context, not this request's (see compactor).
	if job := c.startOrJoin(req, opts); job != nil { //nolint:contextcheck // deliberately outlives the request
		select {
		case <-job.done:
		case <-ctx.Done():
			return llm.Response{}, ctx.Err() //nolint:wrapcheck // the coordinator drops a canceled request
		}
		if job.interrupted {
			// The stop that interrupted it cancels this request next.
			<-ctx.Done()

			return llm.Response{}, ctx.Err() //nolint:wrapcheck // as above
		}
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

// startOrJoin returns the compaction job this request waits for: the one in
// progress, or a new one when a compaction is due; nil when none is.
func (c *compactor) startOrJoin(req llm.Request, opts llm.RequestOptions) *compactionJob {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.job != nil {
		return c.job
	}
	trigger, used, ok := c.dueLocked(req.Input)
	if !ok {
		return nil
	}
	c.pending = false
	ctx, cancel := context.WithCancel(c.ctx)
	job := &compactionJob{done: make(chan struct{}), cancel: cancel}
	c.job = job
	go c.run(ctx, job, req, opts, trigger, used)

	return job
}

// dueLocked reports whether to compact before this request, why, and the
// context in use. Automatic compaction needs something new to summarize, so
// a history that stays large after compacting does not compact on every
// request. It holds c.mu.
func (c *compactor) dueLocked(input []llm.Item) (compaction.Trigger, int64, bool) {
	covered := 0
	view := input
	if c.record != nil && !c.stale {
		if out, err := compaction.Apply(input, *c.record); err == nil {
			covered, view = c.record.Covered, out
		}
	}
	used := compaction.InUse(view, c.used)
	if c.pending {
		return compaction.TriggerManual, used, true
	}
	limit := compaction.AutoLimit(compaction.ContextWindow(c.next.currentModel(), c.window), c.percent)
	if limit == 0 || used < limit || c.autoFailures >= maxAutoFailures || compaction.Coverable(input) <= covered {
		return "", 0, false
	}

	return compaction.TriggerAuto, used, true
}

// run summarizes the history as the model would see it, records the
// compaction, and reports it. On failure the request goes out uncompacted.
func (c *compactor) run(ctx context.Context, job *compactionJob, req llm.Request, opts llm.RequestOptions, trigger compaction.Trigger, used int64) {
	defer close(job.done)
	defer job.cancel()
	c.emit(engine.CompactionStarted{At: time.Now(), Trigger: trigger, Tokens: used})
	rec, err := c.compact(ctx, req, opts, trigger)
	interrupted := err != nil && ctx.Err() != nil
	job.interrupted = interrupted
	c.mu.Lock()
	c.job = nil
	switch {
	case err == nil:
		c.record, c.stale, c.used, c.autoFailures = &rec, false, 0, 0
	case trigger == compaction.TriggerAuto && !interrupted:
		c.autoFailures++
	}
	c.mu.Unlock()
	if err != nil {
		if interrupted {
			err = fmt.Errorf("interrupted: %w", err)
		}
		c.emit(engine.Compacted{At: time.Now(), Trigger: trigger, Err: err.Error(), Interrupted: interrupted})

		return
	}
	c.emit(engine.Compacted{At: time.Now(), Trigger: trigger, Summary: rec.Summary})
}

func (c *compactor) compact(ctx context.Context, req llm.Request, opts llm.RequestOptions, trigger compaction.Trigger) (compaction.Record, error) {
	if c.before != nil {
		if err := c.before(ctx, trigger); err != nil {
			return compaction.Record{}, fmt.Errorf("compaction was stopped: %w", err)
		}
	}
	covered := compaction.Coverable(req.Input)
	if covered == 0 {
		return compaction.Record{}, errNothingToCompact
	}
	summarize := c.summarize
	if summarize == nil {
		summarize = c.localSummary(req.Model.ReasoningEffort, opts.CacheKey)
	}
	summary, err := summarize(ctx, c.apply(req.Input[:1+covered]))
	if err != nil {
		return compaction.Record{}, err
	}
	rec, err := compaction.NewRecord(req.Input, summary, trigger, c.next.currentModel(), time.Now().UTC())
	if err != nil {
		return compaction.Record{}, err
	}
	if err := c.log.Append(rec); err != nil {
		return compaction.Record{}, err
	}

	return rec, nil
}

// localSummary asks the session's current model, as Codex's local
// compaction does.
func (c *compactor) localSummary(effort llm.ReasoningEffort, cacheKey string) compaction.Summarizer {
	return func(ctx context.Context, view []llm.Item) (string, error) {
		return compaction.Summarize(ctx, compaction.SummaryCall{ //nolint:wrapcheck // Summarize wraps its errors
			Adapter: c.next.direct(), Effort: effort, CacheKey: cacheKey,
			Window: compaction.ContextWindow(c.next.currentModel(), c.window),
		}, view)
	}
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
	report := !c.stale
	c.stale = true
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

// lastUsage is the context the session's last model response used (input
// plus output tokens), so automatic compaction also works on a resumed run.
func lastUsage(ctx context.Context, store sessionstore.Store, id session.ID) (int64, error) {
	var used int64
	after := sessionstore.BeforeFirst
	for {
		page, err := store.Items(ctx, id, after, 512)
		if err != nil {
			return 0, fmt.Errorf("failed to read the session: %w", err)
		}
		for _, item := range page.Items {
			if r, ok := item.Data.(sessionstore.ModelResponse); ok {
				used = r.Response.Usage.InputTokens + r.Response.Usage.OutputTokens
			}
		}
		if !page.More {
			return used, nil
		}
		after = page.NextAfter
	}
}

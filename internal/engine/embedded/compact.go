package embedded

import (
	"context"
	"errors"
	"log/slog"
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
	// logger gets a line per compaction with its stats (nil: none).
	logger *slog.Logger
	// remote compacts into the provider's encrypted item (compactremote.go).
	remote bool
	// summarize is the summary call; compaction.Summarize over the
	// switcher's current client when nil. A remote strategy plugs in here.
	summarize compaction.Summarizer
	// before runs as each compaction starts; an error cancels it. It is where
	// a PreCompact hook attaches.
	before func(context.Context, compaction.Trigger) error
	window int64 // the configured window; 0 uses the model catalog
	// windows finds a model's window in the model catalog.
	windows compaction.WindowLookup
	// settings are the automatic limit, the summary model and prompt, and
	// the cap on kept user messages.
	settings compaction.Settings

	mu     sync.Mutex
	record *compaction.Record
	// older are the saved compactions before record, oldest first, and cuts
	// the session's rewinds: a rewind can leave record stale (settleLocked).
	older   []compaction.Record
	cuts    compaction.Cuts
	settled bool
	stale   bool // the record does not match the history; reported once
	// pending is the compaction asked for, "" when none: manual (/compact)
	// or clear (/clear), which wins over manual. focus is what a /compact
	// asked the summary to focus on.
	pending compaction.Trigger
	focus   string
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

// requestCompaction compacts (or clears) before the next model request;
// focus is what the summary should focus on (manual only).
func (c *compactor) requestCompaction(t compaction.Trigger, focus string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending != compaction.TriggerClear {
		c.pending, c.focus = t, focus
	}
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
	resp, err := c.next.Respond(c.withItem(ctx), req, opts)
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
	focus := c.focus
	c.pending, c.focus = "", ""
	ctx, cancel := context.WithCancel(c.ctx)
	job := &compactionJob{done: make(chan struct{}), cancel: cancel}
	c.job = job
	go c.run(ctx, job, req, opts, compactionAsk{trigger: trigger, focus: focus, used: used})

	return job
}

// dueLocked reports whether to compact before this request, why, and the
// context in use. Automatic compaction needs something new to summarize, so
// a history that stays large after compacting does not compact on every
// request. It holds c.mu.
func (c *compactor) dueLocked(input []llm.Item) (compaction.Trigger, int64, bool) {
	c.settleLocked(input)
	covered := 0
	view := input
	if c.record != nil && !c.stale {
		if out, err := compaction.Apply(input, *c.record); err == nil {
			covered, view = c.record.Covered, out
		}
	}
	used := compaction.InUse(view, c.used)
	if c.pending != "" {
		return c.pending, used, true
	}
	limit := c.autoLimit()
	if limit == 0 || used < limit || c.autoFailures >= maxAutoFailures || compaction.Coverable(input) <= covered {
		return "", 0, false
	}

	return compaction.TriggerAuto, used, true
}

// compactionAsk is one compaction to run: why, the user's focus for a
// /compact, and the context in use when it was due.
type compactionAsk struct {
	trigger compaction.Trigger
	focus   string
	used    int64
}

// autoLimit is the tokens in use at which automatic compaction starts for
// the current model; 0 means never.
func (c *compactor) autoLimit() int64 {
	return c.settings.Limit(compaction.ContextWindow(c.next.currentModel(), c.window, c.windows))
}

// apply rewrites the input with the latest compaction. A history that does
// not match it (a session file changed outside uah) goes out in full, and
// the mismatch is reported once.
func (c *compactor) apply(input []llm.Item) []llm.Item {
	c.mu.Lock()
	c.settleLocked(input)
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
	items, err := allItems(ctx, store, id)
	if err != nil {
		return 0, err
	}

	return usageIn(items), nil
}

// usageIn is the context the last model response among the items used.
func usageIn(items []sessionstore.Item) int64 {
	var used int64
	for _, item := range items {
		if r, ok := item.Data.(sessionstore.ModelResponse); ok {
			used = r.Response.Usage.InputTokens + r.Response.Usage.OutputTokens
		}
	}

	return used
}

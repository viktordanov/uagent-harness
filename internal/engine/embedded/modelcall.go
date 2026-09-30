package embedded

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
)

// The runner's client retries in its own loop, silently, and drops the
// stream's deltas (responsesapi/stream.go in v0.1.1). Every attempt goes
// through uah's *http.Client, so each model request gets one observer, a
// modelCall in its context, which the transport (transport.go) and the
// scanner (sse.go) report to. See docs/design/streaming.md.

// The kinds of model request: the coordinator's turns, and direct calls
// (a compaction summary, an auto-review), which report only their retries.
const kindTurn, kindDirect = "turn", "direct"

// callKey carries a request's modelCall to the transport.
type callKey struct{}

// modelCall observes one model request. It queues events; a pump sends
// them to emit, merged when they pile up.
type modelCall struct {
	ctx        context.Context
	kind       string
	emit       func(core.Event)
	text       bool        // stream the text deltas
	log        *searchLog  // record the web searches
	remote     *remoteCall // a remote compaction's answer
	max        int
	diag       io.Writer // a line per attempt: the run's stderr.log
	wake, done chan struct{}

	mu       sync.Mutex
	pending  []core.Event
	closed   bool
	streamed bool            // text went out since the last reset
	retrying bool            // a Reconnecting event is showing
	lost     bool            // the last attempt lost its connection
	final    map[string]bool // the message items that are the final answer
	outputs  attemptOutputs
	tool     toolCall
	// a is the attempt, last its last data, shown the last ModelProgress.
	a           attemptDiag
	last, shown time.Time
	terminal    string   // the stream's terminal event
	failure     apiError // a failed response's error
}

// attemptDiag is an attempt's line in the diagnostics.
type attemptDiag struct {
	Diag          string    `json:"diag"`
	At            time.Time `json:"at"`
	Kind          string    `json:"kind"`
	Attempt       int       `json:"attempt"`
	Max           int       `json:"max"`
	SentBytes     int64     `json:"sent_bytes"`
	ConnectMS     int64     `json:"connect_ms"`
	FirstByteMS   int64     `json:"first_byte_ms"`
	Status        int       `json:"status"`
	Events        int       `json:"events"`
	RecvBytes     int64     `json:"recv_bytes"`
	LongestGapMS  int64     `json:"longest_gap_ms"`
	Tool          string    `json:"tool,omitempty"`
	NetworkWaitMS int64     `json:"network_wait_ms"`
	Result        string    `json:"result"`
	Reason        string    `json:"reason,omitempty"`
	DelayMS       int64     `json:"delay_ms,omitempty"`
}

// observe gives a request its modelCall; the func it returns sends what is
// left, so every event precedes the runner's final events for the response.
func (s *switcher) observe(ctx context.Context, kind string) (context.Context, func(error) error) {
	c := &modelCall{ctx: ctx, kind: kind, emit: s.stream, max: s.max, diag: s.diag, wake: make(chan struct{}, 1), done: make(chan struct{}), final: map[string]bool{}}
	if kind == kindTurn {
		c.text, c.log = s.text, s.searches
	}
	c.remote, _ = ctx.Value(remoteCallKey{}).(*remoteCall)
	go func() {
		defer close(c.done)
		for open := true; open; {
			_, open = <-c.wake
			c.mu.Lock()
			events := c.pending
			c.pending = nil
			c.mu.Unlock()
			for _, e := range events {
				c.emit(e)
			}
		}
	}()

	return context.WithValue(ctx, callKey{}, c), c.end
}

// pushLocked queues an event, merged into the one before when it can be.
func (c *modelCall) pushLocked(e core.Event) {
	if c.closed || c.emit == nil {
		return
	}
	if n := len(c.pending); n > 0 {
		if merged, ok := mergeDelta(c.pending[n-1], e); ok {
			c.pending[n-1] = merged

			return
		}
	}
	c.pending = append(c.pending, e)
	select {
	case c.wake <- struct{}{}:
	default: // the pump is already due
	}
}

// mergeDelta joins deltas of the same text; a ModelProgress replaces one.
func mergeDelta(prev, next core.Event) (core.Event, bool) {
	switch p := prev.(type) {
	case engine.TextDelta:
		if n, ok := next.(engine.TextDelta); ok && n.ItemID == p.ItemID {
			p.Text += n.Text

			return p, true
		}
	case engine.ReasoningDelta:
		if n, ok := next.(engine.ReasoningDelta); ok && n.ItemID == p.ItemID && n.Part == p.Part {
			p.Text += n.Text

			return p, true
		}
	case engine.ModelProgress:
		_, ok := next.(engine.ModelProgress)

		return next, ok
	}

	return nil, false
}

// progressLocked reports a turn's phase, unless forced at most every
// second, or 200 ms while a tool call is being written.
func (c *modelCall) progressLocked(phase string, force bool) {
	now, gap, bytes := time.Now(), time.Second, c.a.RecvBytes
	if c.tool.name != "" {
		gap = 200 * time.Millisecond
	}
	if c.kind != kindTurn || !force && now.Sub(c.shown) < gap {
		return
	}
	if phase == engine.PhaseSending {
		bytes = c.a.SentBytes
	}
	c.shown = now
	c.pushLocked(engine.ModelProgress{At: c.last, Phase: phase, Attempt: c.a.Attempt, Bytes: bytes, Tool: c.tool.name, Target: c.tool.target, ToolBytes: c.tool.bytes})
}

// phase reports a new phase, and its time into the attempt in ms.
func (c *modelCall) phase(phase string, ms *int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ms != nil {
		*ms = time.Since(c.a.At).Milliseconds()
	}
	c.progressLocked(phase, true)
}

// start begins an attempt: the one before it was a retry, its text void.
func (c *modelCall) start(sent int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeLocked("retry")
	c.a = attemptDiag{Diag: "model_attempt", At: time.Now(), Kind: c.kind, Attempt: c.a.Attempt + 1, Max: c.max, SentBytes: sent}
	c.last, c.lost, c.terminal, c.failure, c.tool, c.outputs = c.a.At, false, "", apiError{}, toolCall{}, attemptOutputs{}
	c.remote.reset()
	c.resetLocked()
	c.progressLocked(engine.PhaseConnecting, true)
}

// resetLocked voids the text streamed so far.
func (c *modelCall) resetLocked() {
	if c.streamed {
		c.streamed = false
		c.pushLocked(engine.StreamReset{At: time.Now()})
	}
}

// ended reports a stream's end without its completed response as a failure.
func (c *modelCall) ended(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case c.terminal == eventCompleted || c.terminal == eventIncomplete:
	case c.terminal != "":
		c.failLocked(c.failure.String(), false, c.failure, nil)
	case err != nil && !errors.Is(err, io.EOF):
		c.failLocked(err.Error(), true, apiError{}, nil)
	default:
		c.failLocked("the stream ended before the response completed", false, apiError{}, nil)
	}
}

// failLocked reports an attempt the client retries after retryDelay.
func (c *modelCall) failLocked(reason string, lost bool, e apiError, h http.Header) {
	if c.ctx.Err() != nil {
		return
	}
	c.lost, c.a.Reason = lost, reason
	if c.a.Attempt < c.max {
		delay := retryDelay(c.a.Attempt, e, h)
		c.a.DelayMS, c.retrying = delay.Milliseconds(), true
		c.pushLocked(engine.Reconnecting{At: time.Now(), Attempt: c.a.Attempt + 1, MaxAttempts: c.max, Delay: delay, Reason: reason})
	}
}

// end ends the request: a failed one's text is void, since the runner
// records nothing, and one whose every attempt lost its connection says so.
func (c *modelCall) end(err error) error {
	if err == nil {
		c.record()
	}
	c.mu.Lock()
	result := "ok"
	if err != nil {
		result = "failed"
		c.resetLocked()
	}
	if c.ctx.Err() != nil {
		result = "canceled"
	}
	c.writeLocked(result)
	if c.retrying {
		c.pushLocked(engine.ReconnectEnded{At: time.Now(), OK: err == nil})
	}
	c.progressLocked(engine.PhaseDone, true)
	gaveUp := err != nil && c.ctx.Err() == nil && c.max > 1 && c.a.Attempt >= c.max && c.lost
	c.closed = true
	close(c.wake)
	c.mu.Unlock()
	<-c.done
	if gaveUp {
		return &connectionLostError{attempts: c.max, err: err}
	}

	return err
}

// writeLocked writes the attempt's diagnostics, if one started.
func (c *modelCall) writeLocked(result string) {
	c.a.Result = result
	if line, err := json.Marshal(c.a); err == nil && c.diag != nil && c.a.Attempt > 0 {
		_, _ = c.diag.Write(append(line, '\n'))
	}
}

var tryAgainIn = regexp.MustCompile(`(?i)\btry again in\s*(\d+(?:\.\d+)?)\s*(ms|milliseconds?|s|seconds?)\b`)

// retryDelay is the runner's wait after attempt n (responseRetryDelay in
// responsesapi/retry.go, v0.1.1) without the jitter, which only shortens it.
func retryDelay(n int, e apiError, h http.Header) time.Duration {
	hint, after := time.Duration(0), strings.TrimSpace(h.Get("Retry-After"))
	if secs, err := strconv.Atoi(after); err == nil {
		hint = time.Duration(secs) * time.Second
	} else if t, err := http.ParseTime(after); err == nil {
		hint = time.Until(t)
	}
	if m := tryAgainIn.FindStringSubmatch(e.Message); m != nil && e.Code == "rate_limit_exceeded" {
		d, _ := time.ParseDuration(m[1] + strings.TrimSuffix(strings.ToLower(m[2][:1]), "s") + "s") // ms or s
		hint = max(hint, d)
	}
	if hint > 0 {
		return min(hint, retryPolicy.MaxBackoff)
	}
	policy := retryPolicy
	if e.Code == "server_is_overloaded" || e.Code == "slow_down" {
		policy.InitialBackoff, policy.MaxBackoff = 10*time.Second, time.Minute
	}

	return policy.Backoff(n)
}

// connectionLostError is a model request whose every attempt lost its
// connection.
type connectionLostError struct {
	attempts int
	err      error
}

func (e *connectionLostError) Error() string {
	return fmt.Sprintf("gave up after %d attempts because the connection to the model was lost: %v", e.attempts, e.err)
}

func (e *connectionLostError) Unwrap() error { return e.err }

// runError is the error a failed run reports: a lost connection without
// the coordinator's wrapping, so it reads plainly.
func runError(err error) error {
	if lost, ok := errors.AsType[*connectionLostError](err); ok {
		return lost
	}

	return err
}

package embedded

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/primitives"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
)

// The runner's Responses client retries a failed model request in its own
// loop and reports nothing while it waits (responsesapi/stream.go in
// v0.1.1). Every attempt goes through the *http.Client uah builds for it
// (providers.go), so watchTransport sees each attempt, and watched gives
// each request a tracker, through the request's context, that turns the
// attempts into engine.Reconnecting and engine.ReconnectEnded.

// retryPolicy is the runner's retry policy for model requests: its backoff
// and the HTTP statuses it retries.
var retryPolicy = primitives.DefaultRemoteRequest("", "", "").RetryPolicy

// attemptsKey carries a request's tracker to the transport.
type attemptsKey struct{}

// attempts tracks one model request's attempts.
type attempts struct {
	// ctx is the request's own context, to tell a cancel from a failure.
	ctx  context.Context
	max  int
	emit func(core.Event)

	mu       sync.Mutex
	sent     int
	lost     bool // the last attempt lost its connection
	retrying bool // a Reconnecting event is showing
}

// start counts an attempt and returns its number.
func (a *attempts) start() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sent++
	a.lost = false

	return a.sent
}

// failed reports that attempt n failed; the client tries again after the
// runner's backoff, or after the response's Retry-After within the cap.
func (a *attempts) failed(n int, reason string, lost bool, header http.Header) {
	if a.ctx.Err() != nil {
		return
	}
	a.mu.Lock()
	a.lost = lost
	last := n >= a.max
	if !last {
		a.retrying = true
	}
	a.mu.Unlock()
	if last {
		return
	}
	delay := retryPolicy.Backoff(n)
	if hint := retryAfter(header); hint > 0 {
		delay = min(hint, retryPolicy.MaxBackoff)
	}
	a.emit(engine.Reconnecting{At: time.Now(), Attempt: n + 1, MaxAttempts: a.max, Delay: delay, Reason: reason})
}

// end reports that the retries are over, if there were any.
func (a *attempts) end(ok bool) {
	a.mu.Lock()
	was := a.retrying
	a.retrying = false
	a.mu.Unlock()
	if was {
		a.emit(engine.ReconnectEnded{At: time.Now(), OK: ok})
	}
}

// gaveUp reports whether every attempt was sent and the last one lost its
// connection.
func (a *attempts) gaveUp() bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.sent >= a.max && a.lost
}

// retryAfter is a Retry-After header in seconds (0 when absent).
func retryAfter(h http.Header) time.Duration {
	secs, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After")))
	if err != nil || secs <= 0 {
		return 0
	}

	return time.Duration(secs) * time.Second
}

// watchTransport reports each attempt of a tracked request: a connection
// that fails or drops mid-stream, a status the client retries, or a
// response that starts.
type watchTransport struct{ base http.RoundTripper }

func (t watchTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	a, _ := req.Context().Value(attemptsKey{}).(*attempts)
	if a == nil {
		return t.base.RoundTrip(req) //nolint:wrapcheck // a transport returns its base's errors unchanged
	}
	n := a.start()
	resp, err := t.base.RoundTrip(req)
	switch {
	case err != nil:
		a.failed(n, err.Error(), true, nil)
	case slices.Contains(retryPolicy.RetryableStatusCodes, resp.StatusCode):
		a.failed(n, resp.Status, false, resp.Header)
	case resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices:
		a.end(true)
		resp.Body = &watchedBody{ReadCloser: teed(req.Context(), resp.Body), a: a, n: n}
	}

	return resp, err //nolint:wrapcheck // a transport returns its base's errors unchanged
}

// watchedBody reports a stream that drops before it ends.
type watchedBody struct {
	io.ReadCloser

	a    *attempts
	n    int
	once sync.Once
}

func (b *watchedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		b.once.Do(func() { b.a.failed(b.n, err.Error(), true, nil) })
	}

	return n, err //nolint:wrapcheck // a body returns its base's errors unchanged
}

// watched is a client whose requests report their retries to emit and
// whose final error says when the connection was lost.
type watched struct {
	Client

	max  int
	emit func(core.Event)
}

func (w watched) Respond(ctx context.Context, req llm.Request, opts llm.RequestOptions) (llm.Response, error) {
	emit := w.emit
	if emit == nil {
		emit = func(core.Event) {}
	}
	a := &attempts{ctx: ctx, max: w.max, emit: emit}
	resp, err := w.Client.Respond(context.WithValue(ctx, attemptsKey{}, a), req, opts)
	a.end(err == nil)
	if err != nil && ctx.Err() == nil && w.max > 1 && a.gaveUp() {
		return resp, &connectionLostError{attempts: w.max, err: err}
	}

	return resp, err //nolint:wrapcheck // the coordinator wraps model errors
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

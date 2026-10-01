package embedded

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/llm/responsesapi"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/testing/fakellm"
)

// These tests change the package's limits, so none runs in parallel.

// setLimit sets a package limit for the test.
func setLimit(t *testing.T, limit *time.Duration, d time.Duration) {
	t.Helper()
	old := *limit
	*limit = d
	t.Cleanup(func() { *limit = old })
}

// ask sends one turn request to url through a switcher whose clients use
// headerTimeout, with diagnostics to diag, and returns the request's events
// and error.
func ask(t *testing.T, url string, headerTimeout time.Duration, diag io.Writer) ([]core.Event, error) {
	t.Helper()
	const attempts = 3
	ra, err := newClient(remoteHTTPClient(nil, headerTimeout), ClientConfig{MaxAttempts: attempts}, responsesapi.Config{
		Endpoint: url + "/responses", Headers: map[string][]string{headerContentType: {contentJSON}},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ra.Close() })
	sw, err := newSwitcher("m", variant{}, attempts, func(variant) (Client, error) { return ra, nil })
	require.NoError(t, err)
	var mu sync.Mutex
	var events []core.Event
	sw.diag, sw.stream = diag, func(e core.Event) { mu.Lock(); events = append(events, e); mu.Unlock() }
	_, err = sw.Respond(t.Context(), llm.Request{
		Model: llm.Model{ID: "m"},
		Input: []llm.Item{{Type: llm.ItemMessage, Data: llm.Message{Role: llm.RoleUser, Text: "hi"}}},
	}, llm.RequestOptions{})
	mu.Lock()
	defer mu.Unlock()

	return events, err
}

func only[T core.Event](events []core.Event) []T {
	var out []T
	for _, e := range events {
		if v, ok := e.(T); ok {
			out = append(out, v)
		}
	}

	return out
}

// TestModelCall_RetriesVisibly: each way an attempt fails without a lost
// connection is retried, and reported with its reason and the runner's
// delay: a silent stream, an in-band failure, a stream without its
// completed response, and a status with Retry-After.
func TestModelCall_RetriesVisibly(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the runner's backoff, about 6 s")
	}
	setLimit(t, &streamIdleTimeout, 300*time.Millisecond)
	for _, tc := range []struct {
		name   string
		reply  fakellm.Reply
		reason string
		delay  time.Duration
	}{
		{"silent", fakellm.Reply{Deltas: []string{"a"}, Hold: make(chan struct{})}, "no data from the model for 300ms", 2 * time.Second},
		{"in-band", fakellm.Reply{Fail: http.StatusOK, FailCode: "server_error"}, "server_error: fakellm: server_error", 2 * time.Second},
		{"no end", fakellm.Reply{Deltas: []string{"a"}, NoEnd: true}, "the stream ended before the response completed", 2 * time.Second},
		{"retry-after", fakellm.Reply{Fail: http.StatusTooManyRequests, RetryAfter: "1"}, "429 Too Many Requests", time.Second},
		{"overloaded", fakellm.Reply{Fail: http.StatusOK, FailCode: "server_is_overloaded"}, "server_is_overloaded", 10 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := fakellm.New(t, tc.reply, fakellm.Reply{Text: "back"})
			if tc.delay > 5*time.Second { // only the reported delay is checked
				srv = fakellm.New(t, tc.reply, fakellm.Reply{Fail: http.StatusBadRequest, FailCode: "invalid_prompt"})
			}
			events, err := ask(t, srv.URL, 0, nil)
			retries := only[engine.Reconnecting](events)
			require.NotEmpty(t, retries)
			assert.Equal(t, 2, retries[0].Attempt)
			assert.Contains(t, retries[0].Reason, tc.reason)
			assert.Equal(t, tc.delay, retries[0].Delay)
			if tc.delay > 5*time.Second {
				return
			}
			require.NoError(t, err)
			ended := only[engine.ReconnectEnded](events)
			require.NotEmpty(t, ended)
			assert.True(t, ended[len(ended)-1].OK)
		})
	}
}

// TestModelCall_HeaderTimeout: a server that takes the request and never
// answers times out after responseHeaderTimeout, and the retry answers.
func TestModelCall_HeaderTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the runner's backoff, about 2 s")
	}
	srv := fakellm.New(t, fakellm.Reply{Freeze: true}, fakellm.Reply{Text: "back"})
	events, err := ask(t, srv.URL, 300*time.Millisecond, nil)
	require.NoError(t, err)
	retries := only[engine.Reconnecting](events)
	require.Len(t, retries, 1)
	assert.Contains(t, retries[0].Reason, "timeout awaiting response headers")
	assert.Len(t, srv.Requests(), 2)
}

// TestModelCall_WaitsForTheNetwork: while the network is unreachable the
// request waits, within its first attempt, and reports it; then it goes.
func TestModelCall_WaitsForTheNetwork(t *testing.T) {
	srv := fakellm.New(t)
	addr := strings.TrimPrefix(srv.URL, "http://")
	fails := 2
	var mu sync.Mutex
	old := dialContext
	dialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		mu.Lock()
		defer mu.Unlock()
		if fails > 0 {
			fails--

			return nil, &net.OpError{Op: "dial", Net: network, Err: syscall.ENETUNREACH}
		}

		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	t.Cleanup(func() { dialContext = old })
	setLimit(t, &offlineWait, 10*time.Millisecond)

	var diag bytes.Buffer
	events, err := ask(t, "http://model.test", 0, &diag)
	require.NoError(t, err)
	waits := only[engine.Reconnecting](events)
	require.Len(t, waits, 2)
	for _, w := range waits {
		assert.True(t, w.Offline)
		assert.Equal(t, 1, w.Attempt, "waiting uses up no attempt")
		assert.Contains(t, w.Reason, "waiting for network")
	}
	assert.Len(t, srv.Requests(), 1)
	var line struct {
		Diag, Kind, Result string
		Attempt, Status    int
		NetworkWaitMS      int64 `json:"network_wait_ms"`
	}
	require.NoError(t, json.Unmarshal(diag.Bytes(), &line), "one line for the one attempt")
	assert.Equal(t, [3]string{"model_attempt", "turn", "ok"}, [3]string{line.Diag, line.Kind, line.Result})
	assert.Equal(t, [2]int{1, http.StatusOK}, [2]int{line.Attempt, line.Status})
	assert.Positive(t, line.NetworkWaitMS)
	ended := only[engine.ReconnectEnded](events)
	require.Len(t, ended, 1)
	assert.True(t, ended[0].OK)
}

// TestModelCall_ToolProgress: an apply_patch being written reports its
// file and size, and a progress without a tool once it is written.
func TestModelCall_ToolProgress(t *testing.T) {
	args := `{"input":"*** Begin Patch\n*** Add File: a.go\n+package a\n*** End Patch"}`
	// The header ends in the second piece; the pause lets its progress out
	// before a later one replaces it in the queue.
	cut := strings.Index(args, `a.go\n`) + len(`a.go\n`)
	srv := fakellm.New(t, fakellm.Reply{
		Calls:     []fakellm.Call{{Name: "apply_patch", Args: args}},
		ArgDeltas: []string{args[:20], args[20:cut], args[cut:]},
		Pace:      100 * time.Millisecond,
	})
	events, err := ask(t, srv.URL, 0, nil)
	require.NoError(t, err)
	var writing, written bool
	for _, p := range only[engine.ModelProgress](events) {
		if p.Tool == "apply_patch" && p.Target == "a.go" {
			writing = true
			assert.Positive(t, p.ToolBytes)
		}
		if writing && p.Tool == "" {
			written = true
		}
	}
	assert.True(t, writing, "the patch's file")
	assert.True(t, written, "cleared once written")
}

// TestModelTransport: both kinds of model client, keyed and codex (under
// the login's transport), have the timeouts and share an engine's
// transport; a loopback server gets no header timeout.
func TestModelTransport(t *testing.T) {
	var ts transports
	shared := ts.get(responseHeaderTimeout)
	for _, hc := range []*http.Client{remoteHTTPClient(&ts, headerTimeout("https://api.openai.com/v1")), codexHTTPClient(&ts, nil, "")} {
		ct, ok := hc.Transport.(callTransport)
		require.True(t, ok)
		base := reflect.ValueOf(ct.base)
		if base.Kind() == reflect.Struct { // the codex login's transport
			base = base.FieldByName("base").Elem()
		}
		assert.Equal(t, reflect.ValueOf(shared).Pointer(), base.Pointer())
		tr := base.Elem()
		assert.Equal(t, int64(responseHeaderTimeout), tr.FieldByName("ResponseHeaderTimeout").Int())
		h2 := tr.FieldByName("HTTP2").Elem()
		assert.Equal(t, int64(30*time.Second), h2.FieldByName("SendPingTimeout").Int())
		assert.Equal(t, int64(15*time.Second), h2.FieldByName("PingTimeout").Int())
		assert.Equal(t, int64(time.Minute), h2.FieldByName("WriteByteTimeout").Int())
		assert.True(t, tr.FieldByName("DialContext").IsValid())
	}
	assert.Zero(t, headerTimeout("http://127.0.0.1:11434"))
	assert.NotSame(t, shared, ts.get(0))
	assert.NotSame(t, shared, (*transports)(nil).get(responseHeaderTimeout))
}

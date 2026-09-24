package usage_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/usage"
)

// testToken is a test access token (not a real one) with an account ID and
// an expiry an hour away.
func testToken() string {
	claims := `{"exp":` + strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10) +
		`,"https://api.openai.com/auth":{"chatgpt_account_id":"acct-123"}}`

	return "x." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".y"
}

// codexEnv serves the Codex credentials as the environment would.
func codexEnv(key string) string {
	if key == "OPENAI_CODEX_ACCESS_TOKEN" {
		return testToken()
	}

	return ""
}

// usageServer serves a fixture as the usage endpoint and counts requests;
// gate, when set, holds each request until it is closed.
func usageServer(t *testing.T, name string, gate <-chan struct{}) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	body := fixture(t, name)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/api/codex/usage" || r.Header.Get("ChatGPT-Account-ID") != "acct-123" {
			http.NotFound(w, r)

			return
		}
		if gate != nil {
			<-gate
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	return srv, &calls
}

// clock is a test clock that moves only when told.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }

func (c *clock) Add(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

func TestCodexReader_Caches(t *testing.T) {
	srv, calls := usageServer(t, "pro_weekly_only.json", nil)
	c := &clock{now: captured}
	r := usage.For(usage.Provider, usage.ReaderOptions{Getenv: codexEnv, BaseURL: srv.URL, Now: c.Now})

	s, err := r.Usage(t.Context(), usage.CacheFor)
	require.NoError(t, err)
	assert.Equal(t, "pro", s.Plan)
	assert.Equal(t, int32(1), calls.Load())

	c.Add(30 * time.Second)
	_, err = r.Usage(t.Context(), usage.CacheFor)
	require.NoError(t, err)
	assert.Equal(t, int32(1), calls.Load(), "within the max age, the cached snapshot")

	c.Add(31 * time.Second)
	s, err = r.Usage(t.Context(), usage.CacheFor)
	require.NoError(t, err)
	assert.Equal(t, int32(2), calls.Load(), "older than the max age, a new read")
	assert.Equal(t, captured.Add(61*time.Second), s.CapturedAt)

	c.Add(time.Second)
	_, err = r.Usage(t.Context(), 0)
	require.NoError(t, err)
	assert.Equal(t, int32(3), calls.Load(), "max age 0 always reads")
}

func TestCodexReader_OneRequestAtATime(t *testing.T) {
	gate := make(chan struct{})
	srv, calls := usageServer(t, "plus_two_windows.json", gate)
	r := usage.NewCodexReader(usage.ReaderOptions{Getenv: codexEnv, BaseURL: srv.URL})

	const readers = 8
	var wg sync.WaitGroup
	plans := make(chan string, readers)
	for range readers {
		wg.Go(func() {
			s, err := r.Usage(t.Context(), 0)
			assert.NoError(t, err)
			plans <- s.Plan
		})
	}
	require.Eventually(t, func() bool { return calls.Load() == 1 }, 5*time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond) // the other readers queue behind the first
	close(gate)
	wg.Wait()
	close(plans)

	assert.Equal(t, int32(1), calls.Load(), "the readers that waited share the one read")
	for p := range plans {
		assert.Equal(t, "plus", p)
	}
}

func TestCodexReader_ErrorsKeepTheLastSnapshot(t *testing.T) {
	srv, _ := usageServer(t, "pro_weekly_only.json", nil)
	c := &clock{now: captured}
	r := usage.NewCodexReader(usage.ReaderOptions{Getenv: codexEnv, BaseURL: srv.URL, Now: c.Now})
	_, err := r.Usage(t.Context(), 0)
	require.NoError(t, err)

	srv.Close()
	c.Add(time.Minute)
	s, err := r.Usage(t.Context(), 0)
	require.Error(t, err)
	assert.Equal(t, "pro", s.Plan, "the last snapshot, to show as stale")
	assert.NotContains(t, err.Error(), testToken()[:10])
}

func TestCodexReader_WaitsForTheContext(t *testing.T) {
	gate := make(chan struct{})
	srv, calls := usageServer(t, "pro_weekly_only.json", gate)
	r := usage.NewCodexReader(usage.ReaderOptions{Getenv: codexEnv, BaseURL: srv.URL})
	go func() { _, _ = r.Usage(context.WithoutCancel(t.Context()), 0) }()
	require.Eventually(t, func() bool { return calls.Load() == 1 }, 5*time.Second, time.Millisecond)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	_, err := r.Usage(ctx, 0)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	close(gate)
}

func TestCodexReader_NoCredentials(t *testing.T) {
	r := usage.NewCodexReader(usage.ReaderOptions{Getenv: func(k string) string {
		if k == "CODEX_HOME" {
			return t.TempDir()
		}

		return ""
	}})
	_, err := r.Usage(t.Context(), 0)
	require.ErrorContains(t, err, "Codex credentials")
}

func TestFor_Unsupported(t *testing.T) {
	for _, provider := range []string{"openai", "ollama", "openrouter", ""} {
		_, err := usage.For(provider, usage.ReaderOptions{}).Usage(t.Context(), 0)
		require.ErrorIs(t, err, usage.ErrUnsupported, provider)
		assert.Equal(t, "usage is not available for "+provider, err.Error())
	}
}

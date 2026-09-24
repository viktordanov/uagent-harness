package models_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/models"
	"github.com/viktordanov/uagent-harness/internal/models/modelstest"
)

// backend is a ChatGPT models endpoint whose list and availability tests
// change; it answers 304 to the current ETag.
type backend struct {
	srv       *httptest.Server
	calls     atomic.Int32
	down      atomic.Bool
	body      atomic.Value
	etag      string
	notModded atomic.Int32
}

func newBackend(t *testing.T, body string) *backend {
	t.Helper()
	b := &backend{etag: `"e1"`}
	b.body.Store(body)
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.calls.Add(1)
		if b.down.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)

			return
		}
		if r.Header.Get("If-None-Match") == b.etag {
			b.notModded.Add(1)
			w.WriteHeader(http.StatusNotModified)

			return
		}
		w.Header().Set("ETag", b.etag)
		_, _ = w.Write([]byte(b.body.Load().(string))) //nolint:forcetypeassert // only strings
	}))
	t.Cleanup(b.srv.Close)

	return b
}

const twoModels = `{"models":[{"slug":"gpt-6-sol","priority":2,"context_window":300000},{"slug":"gpt-6-luna","priority":3}]}`

// clock is a settable time.
type clock struct{ now atomic.Int64 }

func (c *clock) Now() time.Time          { return time.Unix(c.now.Load(), 0) }
func (c *clock) Advance(d time.Duration) { c.now.Add(int64(d / time.Second)) }

func newManager(t *testing.T, dir, base string, c *clock, vars map[string]string) *models.Manager {
	t.Helper()
	if vars == nil {
		vars = map[string]string{"CODEX_HOME": codexHome(t, "acct-test")}
	}

	return models.New(models.Options{Dir: dir, Provider: models.ProviderCodex, BaseURL: base, Getenv: env(vars), Now: c.Now})
}

func TestManager_CacheTTLAndETag(t *testing.T) {
	b := newBackend(t, twoModels)
	c := &clock{}
	c.now.Store(1_000_000)
	dir := t.TempDir()
	m := newManager(t, dir, b.srv.URL, c, nil)
	ctx := context.Background()

	first := m.Catalog(ctx, models.ProviderCodex, models.OnlineIfUncached)
	require.NoError(t, first.Err)
	assert.Equal(t, models.OriginLive, first.Origin)
	assert.Equal(t, []string{"gpt-6-sol", "gpt-6-luna"}, first.IDs())
	assert.Equal(t, int32(1), b.calls.Load())

	// A fresh cache answers without the network, also for a new process.
	again := newManager(t, dir, b.srv.URL, c, nil).Catalog(ctx, models.ProviderCodex, models.OnlineIfUncached)
	assert.Equal(t, models.OriginCached, again.Origin)
	assert.Equal(t, int32(1), b.calls.Load())

	// After the TTL the manager asks again, with the cached ETag; a 304
	// keeps the list and renews it.
	c.Advance(models.DefaultTTL + time.Second)
	renewed := m.Catalog(ctx, models.ProviderCodex, models.OnlineIfUncached)
	assert.Equal(t, models.OriginLive, renewed.Origin)
	assert.Equal(t, first.Models, renewed.Models)
	assert.Equal(t, int32(2), b.calls.Load())
	assert.Equal(t, int32(1), b.notModded.Load())
	assert.Equal(t, models.OriginCached, m.Catalog(ctx, models.ProviderCodex, models.OnlineIfUncached).Origin, "the 304 renewed the TTL")

	// Online always asks.
	m.Catalog(ctx, models.ProviderCodex, models.Online)
	assert.Equal(t, int32(3), b.calls.Load())
}

func TestManager_FailedRefreshFallsBack(t *testing.T) {
	b := newBackend(t, twoModels)
	c := &clock{}
	m := newManager(t, t.TempDir(), b.srv.URL, c, nil)
	ctx := context.Background()

	b.down.Store(true)
	bundled := m.Catalog(ctx, models.ProviderCodex, models.Online)
	assert.Equal(t, models.OriginBundled, bundled.Origin, "no cache: the bundled list")
	require.ErrorContains(t, bundled.Err, "503")
	assert.Contains(t, bundled.IDs(), "gpt-6-sol")
	assert.False(t, bundled.Authoritative())

	b.down.Store(false)
	require.NoError(t, m.Catalog(ctx, models.ProviderCodex, models.Online).Err)
	b.down.Store(true)
	cached := m.Catalog(ctx, models.ProviderCodex, models.Online)
	assert.Equal(t, models.OriginCached, cached.Origin, "then the cache")
	require.Error(t, cached.Err)
	assert.Equal(t, []string{"gpt-6-sol", "gpt-6-luna"}, cached.IDs())
	assert.True(t, cached.Authoritative())
}

func TestManager_RefreshTimeout(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-block }))
	t.Cleanup(func() { close(block); srv.Close() })
	m := models.New(models.Options{
		Provider: models.ProviderCodex, BaseURL: srv.URL, Timeout: 50 * time.Millisecond,
		Getenv: env(map[string]string{"CODEX_HOME": codexHome(t, "a")}),
	})

	start := time.Now()
	got := m.Catalog(context.Background(), models.ProviderCodex, models.Online)

	assert.Less(t, time.Since(start), 2*time.Second)
	assert.Equal(t, models.OriginBundled, got.Origin)
	require.ErrorIs(t, got.Err, context.DeadlineExceeded)
}

// TestManager_IdentityScoping never serves one login's list to another.
func TestManager_IdentityScoping(t *testing.T) {
	b := newBackend(t, twoModels)
	c := &clock{}
	dir := t.TempDir()
	ctx := context.Background()
	newManager(t, dir, b.srv.URL, c, nil).Catalog(ctx, models.ProviderCodex, models.Online)

	other := newManager(t, dir, b.srv.URL, c, map[string]string{"CODEX_HOME": codexHome(t, "acct-other")})
	assert.Equal(t, models.OriginBundled, other.Catalog(ctx, models.ProviderCodex, models.Offline).Origin, "another account misses the cache")
	b.down.Store(true)
	assert.Equal(t, models.OriginBundled, other.Catalog(ctx, models.ProviderCodex, models.OnlineIfUncached).Origin)

	data, err := os.ReadFile(filepath.Join(dir, "openai-codex.json"))
	require.NoError(t, err)
	assert.NotContains(t, string(data), "acct-test", "the identity is hashed")
	assert.NotContains(t, string(data), "x.", "no token")
}

func TestManager_NoCredentials(t *testing.T) {
	m := models.New(models.Options{Provider: models.ProviderOpenAI, Getenv: env(nil)})
	c := m.Catalog(context.Background(), models.ProviderOpenAI, models.Online)
	assert.Equal(t, models.OriginBundled, c.Origin)
	require.ErrorContains(t, c.Err, "OPENAI_API_KEY")
	assert.NoError(t, m.Validate(context.Background(), models.ProviderOpenAI, "gpt-new"), "no list from the provider: pass through")
}

func TestManager_CachedForCompletion(t *testing.T) {
	dir := t.TempDir()
	assert.Equal(t, models.OriginNone, models.New(models.Options{Dir: dir}).Cached(models.ProviderOllama).Origin)

	m2 := models.New(models.Options{
		Dir: dir, NewSource: func(string, string, func(string) string) (models.Source, error) {
			return modelstest.Source{ID: "x", Models: modelstest.IDs("qwen3:8b")}, nil
		},
	})
	m2.Catalog(context.Background(), models.ProviderOllama, models.Online)
	got := models.New(models.Options{Dir: dir}).Cached(models.ProviderOllama)
	assert.Equal(t, []string{"qwen3:8b"}, got.IDs())
}

func TestValidate(t *testing.T) {
	var calls atomic.Int32
	m := modelstest.Manager(t, models.ProviderCodex, modelstest.Source{Models: modelstest.IDs("gpt-6-sol", "gpt-6-luna"), Calls: &calls})
	ctx := context.Background()

	require.NoError(t, m.Validate(ctx, models.ProviderCodex, "gpt-6-luna"))
	err := m.Validate(ctx, models.ProviderCodex, "gpt-luna-6")
	require.ErrorIs(t, err, models.ErrUnavailable)
	require.EqualError(t, err, "Unknown model `gpt-luna-6` for spawn_agent. Available models: gpt-6-sol, gpt-6-luna. Did you mean `gpt-6-luna`?")
	var ue *models.UnavailableError
	require.ErrorAs(t, err, &ue)
	assert.Equal(t, []string{"gpt-6-luna"}, ue.Suggestions)
	require.EqualError(t, m.Validate(ctx, models.ProviderCodex, "zzz"), "Unknown model `zzz` for spawn_agent. Available models: gpt-6-sol, gpt-6-luna")
	assert.Equal(t, int32(1), calls.Load(), "the cache answers the later checks")

	c := m.Catalog(ctx, models.ProviderCodex, models.Offline)
	require.EqualError(t, c.Check("gpt-luna-6"), "gpt-luna-6 is not available on openai-codex; did you mean gpt-6-luna?")
	require.EqualError(t, c.Check("zzz"), "zzz is not available on openai-codex (see `uah models`)")

	many := modelstest.Manager(t, models.ProviderCodex, modelstest.Source{Models: models.Bundled(models.ProviderCodex).Models})
	require.EqualError(t, many.Validate(ctx, models.ProviderCodex, "gpt-luna-6"),
		"Unknown model `gpt-luna-6` for spawn_agent. Available models: gpt-6-astra, gpt-6-sol, gpt-6-luna, gpt-5.6-sol, gpt-5.6-terra. Did you mean `gpt-6-luna`?",
		"Codex's message and its five listed models, as agents.CodexModels gives today")

	down := modelstest.Manager(t, models.ProviderCodex, modelstest.Source{Err: errors.New("offline")})
	require.NoError(t, down.Validate(ctx, models.ProviderCodex, "gpt-7"), "the bundled list never rejects a model")

	models.SetDefault(m)
	t.Cleanup(func() { models.SetDefault(nil) })
	require.ErrorIs(t, models.Validate(ctx, models.ProviderCodex, "gpt-luna-6"), models.ErrUnavailable)
}

func TestWindow(t *testing.T) {
	m := modelstest.Manager(t, models.ProviderOpenRouter, modelstest.Source{Models: []models.Model{{ID: "moonshot/kimi", ContextWindow: 131072}}})
	models.SetDefault(m)
	t.Cleanup(func() { models.SetDefault(nil) })

	_, ok := models.Window("moonshot/kimi")
	assert.False(t, ok, "nothing loaded yet")
	m.Catalog(context.Background(), models.ProviderOpenRouter, models.Online)
	w, ok := models.Window("moonshot/kimi")
	assert.True(t, ok)
	assert.Equal(t, int64(131072), w)
	w, _ = models.Window("gpt-daybreak-red-latest")
	assert.Equal(t, int64(372000), w, "the bundled catalog when the provider's lacks it")
	w, _ = models.Window("openai/gpt-5.5-2026-01-01")
	assert.Equal(t, int64(272000), w, "a namespaced, dated ID takes its base model's metadata, as in Codex")
}

func TestApplyPatch(t *testing.T) {
	m := models.New(models.Options{})
	assert.True(t, m.ApplyPatch(models.ProviderCodex, "gpt-5.5"), "the bundled entry has apply_patch_tool_type")
	md, ok := models.Bundled(models.ProviderCodex).Metadata("gpt-5.5")
	require.True(t, ok)
	assert.Equal(t, "freeform", md.ApplyPatchTool)
	assert.True(t, m.ApplyPatch(models.ProviderOpenAI, "gpt-unlisted"), "OpenAI's providers get it without an entry")
	assert.False(t, m.ApplyPatch("ollama", "llama3"))
}

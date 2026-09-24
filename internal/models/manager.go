package models

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// RefreshTimeout bounds one refresh, credentials included, as Codex bounds
// its refresh after an auth change (models-manager/src/manager.rs).
const RefreshTimeout = 5 * time.Second

// Strategy is when a catalog request may call the provider: Codex's
// RefreshStrategy.
type Strategy int

const (
	// OnlineIfUncached uses a fresh cache, else asks the provider.
	OnlineIfUncached Strategy = iota
	// Online always asks the provider.
	Online
	// Offline never asks: the cache at any age, else the bundled list.
	Offline
)

// Options configure a Manager.
type Options struct {
	// Dir holds the cache files; empty turns the cache off.
	Dir string
	// TTL is how long a list is fresh (default DefaultTTL).
	TTL time.Duration
	// Timeout bounds a refresh (default RefreshTimeout).
	Timeout time.Duration
	// Getenv reads credentials (default os.Getenv).
	Getenv func(string) string
	// Provider is the session's provider: its catalog feeds ContextWindow.
	Provider string
	// BaseURL overrides Provider's base URL, as --base-url does.
	BaseURL string
	// NewSource builds a provider's source (default NewSource).
	NewSource func(provider, baseURL string, getenv func(string) string) (Source, error)
	// Now is the clock (default time.Now).
	Now func() time.Time
}

// Manager serves catalogs: from the provider, the cache, or the bundled
// list, as Codex's OpenAiModelsManager does. It is safe for concurrent use.
type Manager struct {
	opts   Options
	cache  fileCache
	mu     sync.Mutex
	locks  map[string]*sync.Mutex // per provider: one refresh at a time
	active atomic.Pointer[Catalog]
}

// CacheDir is where uah caches model lists in a state directory.
func CacheDir(stateDir string) string { return filepath.Join(stateDir, "models") }

// New returns a manager.
func New(opts Options) *Manager {
	if opts.TTL == 0 {
		opts.TTL = DefaultTTL
	}
	if opts.Timeout <= 0 {
		opts.Timeout = RefreshTimeout
	}
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}
	if opts.NewSource == nil {
		opts.NewSource = NewSource
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}

	return &Manager{opts: opts, cache: fileCache{dir: opts.Dir}}
}

// Provider is the session's provider.
func (m *Manager) Provider() string { return m.opts.Provider }

// Catalog returns the provider's list by the strategy. It never fails: when
// the provider cannot be asked it returns the cache, else the bundled list,
// with Err saying why.
func (m *Manager) Catalog(ctx context.Context, provider string, strategy Strategy) Catalog {
	lock := m.lock(provider)
	lock.Lock()
	defer lock.Unlock()
	c := m.catalog(ctx, provider, strategy)
	if provider == m.opts.Provider && (c.Origin != OriginBundled || m.active.Load() == nil) {
		m.active.Store(&c)
	}

	return c
}

func (m *Manager) lock(provider string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locks == nil {
		m.locks = map[string]*sync.Mutex{}
	}
	if m.locks[provider] == nil {
		m.locks[provider] = &sync.Mutex{}
	}

	return m.locks[provider]
}

func (m *Manager) catalog(ctx context.Context, provider string, strategy Strategy) Catalog {
	baseURL := ""
	if provider == m.opts.Provider {
		baseURL = m.opts.BaseURL
	}
	src, err := m.opts.NewSource(provider, baseURL, m.opts.Getenv)
	if err != nil {
		return withErr(Bundled(provider), err)
	}
	cached, hasCache := m.cache.load(provider, src.Identity())
	fromCache := Catalog{Provider: provider, Origin: OriginCached, Models: cached.Models, FetchedAt: cached.FetchedAt}
	switch {
	case strategy == Offline && hasCache:
		return fromCache
	case strategy == Offline:
		return Bundled(provider)
	case strategy == OnlineIfUncached && hasCache && cached.fresh(m.opts.Now(), m.opts.TTL):
		return fromCache
	}
	fetched, err := m.fetch(ctx, src, cached.ETag)
	switch {
	case err != nil && hasCache:
		return withErr(fromCache, err)
	case err != nil:
		return withErr(Bundled(provider), err)
	case fetched.NotModified && hasCache:
		fetched.Models = cached.Models // the provider confirmed the cached list
	case fetched.NotModified:
		return withErr(Bundled(provider), errors.New("the provider answered 304 Not Modified to a request without a cached list"))
	default:
		fetched.Models = enrich(provider, fetched.Models)
	}
	e := entry{Provider: provider, Identity: src.Identity(), FetchedAt: m.opts.Now(), ETag: fetched.ETag, Models: fetched.Models}
	live := Catalog{Provider: provider, Origin: OriginLive, Models: e.Models, FetchedAt: e.FetchedAt}
	if err := m.cache.store(e); err != nil {
		live.Err = err // the list is still good
	}

	return live
}

func (m *Manager) fetch(ctx context.Context, src Source, etag string) (Fetched, error) {
	ctx, cancel := context.WithTimeout(ctx, m.opts.Timeout)
	defer cancel()

	return src.Fetch(ctx, etag) //nolint:wrapcheck // sources wrap their errors
}

func withErr(c Catalog, err error) Catalog {
	c.Err = err

	return c
}

// Cached is the provider's list from the cache at any age, without reading
// credentials or calling the network, for shell completion; else the
// bundled list.
func (m *Manager) Cached(provider string) Catalog {
	if e, ok := m.cache.peek(provider); ok {
		return Catalog{Provider: provider, Origin: OriginCached, Models: e.Models, FetchedAt: e.FetchedAt}
	}

	return Bundled(provider)
}

// Validate checks that the provider offers the model, for spawn_agent.
// Only a list from the provider (live or cached) can reject it; with none,
// or the bundled list, any model passes, as before the catalog. The error
// is an UnavailableError in Codex's spawn_agent wording, with near misses.
func (m *Manager) Validate(ctx context.Context, provider, model string) error {
	err := m.Catalog(ctx, provider, OnlineIfUncached).Check(model)
	if ue, ok := errors.AsType[*UnavailableError](err); ok {
		ue.Tool = "spawn_agent"
	}

	return err
}

// Window is the model's context window from the session provider's last
// catalog, else the bundled one; ok is false when neither knows it.
func (m *Manager) Window(model string) (int64, bool) {
	if c := m.active.Load(); c != nil {
		if md, ok := c.Metadata(model); ok && md.ContextWindow > 0 {
			return md.ContextWindow, true
		}
	}
	return BundledWindow(model)
}

// ApplyPatch reports whether the provider's model gets Codex's apply_patch
// tool, as Codex offers it when the model's catalog entry has an
// apply_patch_tool_type (spec_plan.rs). The provider's last list answers,
// else the bundled one; on openai and openai-codex, whose catalogs are
// Codex's, a model no entry describes gets it too.
func (m *Manager) ApplyPatch(provider, model string) bool {
	if md, ok := m.Cached(provider).Metadata(model); ok && md.ApplyPatchTool != "" {
		return true
	}

	return provider == ProviderOpenAI || provider == ProviderCodex
}

// ErrUnavailable matches an UnavailableError.
var ErrUnavailable = errors.New("the model is not available")

// maxAvailableShown is how many models Codex names for an unknown one
// (MAX_SPAWN_AGENT_MODEL_OVERRIDES).
const maxAvailableShown = 5

// UnavailableError is a model the provider's list does not have.
type UnavailableError struct {
	Model, Provider string
	// Suggestions are near misses from the list, best first.
	Suggestions []string
	// Available are the first visible models in priority order, at most five.
	Available []string
	// Tool is the tool that asked, such as spawn_agent; the message is then
	// Codex's for that tool.
	Tool string
}

// Error is "X is not available on P; did you mean Y?", or with Tool set
// Codex's "Unknown model `X` for spawn_agent. Available models: …" followed
// by "Did you mean `Y`?".
func (e *UnavailableError) Error() string {
	if e.Tool != "" {
		msg := fmt.Sprintf("Unknown model `%s` for %s. Available models: %s", e.Model, e.Tool, strings.Join(e.Available, ", "))
		if len(e.Suggestions) > 0 {
			msg += ". Did you mean `" + strings.Join(e.Suggestions, "` or `") + "`?"
		}

		return msg
	}
	if len(e.Suggestions) == 0 {
		return fmt.Sprintf("%s is not available on %s (see `uah models`)", e.Model, e.Provider)
	}

	return fmt.Sprintf("%s is not available on %s; did you mean %s?", e.Model, e.Provider, strings.Join(e.Suggestions, " or "))
}

func (e *UnavailableError) Is(target error) bool { return target == ErrUnavailable }

// Check returns an UnavailableError when the list is authoritative and
// lacks the model, else nil.
func (c Catalog) Check(model string) error {
	if !c.Authoritative() {
		return nil
	}
	if _, ok, near := c.Lookup(model); !ok {
		ids := c.IDs()

		return &UnavailableError{Model: model, Provider: c.Provider, Suggestions: near, Available: ids[:min(len(ids), maxAvailableShown)]}
	}

	return nil
}

// BundledWindow is the model's context window in the catalog shipped with
// uah, for callers without a provider's list, such as uah config.
func BundledWindow(model string) (int64, bool) {
	if md, ok := Bundled(ProviderCodex).Metadata(model); ok && md.ContextWindow > 0 {
		return md.ContextWindow, true
	}

	return 0, false
}

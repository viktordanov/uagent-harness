package usage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/llm/clients/openaicodex"

	"github.com/viktordanov/uagent-harness/internal/engine/codexauth"
)

// ErrUnsupported means the provider has no usage uah can read. The error a
// Reader returns wraps it and names the provider: "usage is not available
// for ollama".
var ErrUnsupported = errors.New("usage is not available")

// Reader reads the subscription's usage.
type Reader interface {
	// Usage returns the latest snapshot, reading the backend when the cached
	// one is older than maxAge (0 always reads, unless a read that started
	// after this call already finished). On an error it returns the last
	// snapshot it has (zero without one), so a caller can show it as stale.
	Usage(ctx context.Context, maxAge time.Duration) (Snapshot, error)
}

// CacheFor is how long a snapshot serves reads that are not on demand, such
// as the one after each run.
const CacheFor = 60 * time.Second

// Provider is the only provider with usage: the ChatGPT subscription.
const Provider = "openai-codex"

// ReaderOptions configure a reader. The zero value reads the real backend
// with the environment's Codex login.
type ReaderOptions struct {
	// Getenv finds the Codex credentials as the engine does (default os.Getenv).
	Getenv func(string) string
	// BaseURL is the provider's base URL, as --base-url sets it ("" for the
	// runner's default).
	BaseURL string
	// Client and Now are passed to Fetch.
	Client *http.Client
	Now    func() time.Time
}

// For returns the reader for a provider: the Codex reader for openai-codex,
// else one that returns ErrUnsupported.
func For(provider string, opts ReaderOptions) Reader {
	if provider != Provider {
		return unsupported{provider: provider}
	}

	return NewCodexReader(opts)
}

type unsupported struct{ provider string }

func (u unsupported) Usage(context.Context, time.Duration) (Snapshot, error) {
	return Snapshot{}, fmt.Errorf("%w for %s", ErrUnsupported, u.provider)
}

// CodexReader reads openai-codex usage with Fetch: one request at a time,
// the last snapshot kept in memory. It loads the credentials on each read,
// because Codex refreshes its login file.
type CodexReader struct {
	opts ReaderOptions
	// sem holds the one read in flight; a caller waiting on it gets that
	// read's snapshot.
	sem  chan struct{}
	last Snapshot
	has  bool
}

// NewCodexReader returns a reader for the ChatGPT subscription.
func NewCodexReader(opts ReaderOptions) *CodexReader {
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}

	return &CodexReader{opts: opts, sem: make(chan struct{}, 1)}
}

// Usage implements Reader.
func (r *CodexReader) Usage(ctx context.Context, maxAge time.Duration) (Snapshot, error) {
	called := r.opts.Now()
	select {
	case r.sem <- struct{}{}:
	case <-ctx.Done():
		return Snapshot{}, fmt.Errorf("failed to read the usage: %w", ctx.Err())
	}
	defer func() { <-r.sem }()
	if r.has && (!r.last.CapturedAt.Before(called) || (maxAge > 0 && called.Sub(r.last.CapturedAt) <= maxAge)) {
		return r.last, nil
	}
	s, err := r.fetch(ctx)
	if err != nil {
		return r.last, err
	}
	r.last, r.has = s, true

	return s, nil
}

func (r *CodexReader) fetch(ctx context.Context) (Snapshot, error) {
	config, err := openaicodex.EnvironmentConfig(r.opts.Getenv)
	if err != nil {
		return Snapshot{}, fmt.Errorf("failed to find the Codex credentials: %w", err)
	}
	creds, err := codexauth.Load(config)
	if err != nil {
		return Snapshot{}, fmt.Errorf("failed to read the Codex credentials: %w", err)
	}

	return Fetch(ctx, creds, Options{BaseURL: r.opts.BaseURL, Client: r.opts.Client, Now: r.opts.Now})
}

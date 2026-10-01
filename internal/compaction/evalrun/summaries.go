package evalrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/viktordanov/unreal-agent/harness/llm"

	"github.com/viktordanov/uah/internal/compaction"
	"github.com/viktordanov/uah/internal/engine/embedded"
)

// StubSummary stands in for a summary no source has, so the tables still
// compare what the rest of a strategy keeps. Its facts count for nothing.
const StubSummary = "(no summary offline)"

// Where a summary came from.
const (
	SourceRecorded = "recorded"
	SourceCache    = "cache"
	SourceModel    = "model"
	SourceStub     = "stub"
)

// Summaries finds a summary for a covered history: the session's own
// compaction log (for the default prompt), the cache, a model when Live is
// set (the result is cached), else the stub.
type Summaries struct {
	// Dir caches summaries by the covered items' hash and the prompt ("":
	// no cache).
	Dir string
	// Prompt is the summary prompt ("": Codex's).
	Prompt string
	// Live writes a summary that no other source has; nil never calls a
	// model.
	Live compaction.Summarizer

	mu       sync.Mutex
	recorded map[string]string // covered hash -> summary
	// Sources counts the summaries by source.
	Sources map[string]int
}

// Record adds the summaries of a session's compaction log.
func (s *Summaries) Record(sessionsDir, id string) error {
	records, _, err := compaction.OpenLog(sessionsDir, id).Records()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recorded == nil {
		s.recorded = map[string]string{}
	}
	for _, r := range records {
		if r.Summary != "" && r.Hash != "" {
			s.recorded[r.Hash] = r.Summary
		}
	}

	return nil
}

// Get is the summary of view, the system message and the covered items
// whose hash is hash.
func (s *Summaries) Get(ctx context.Context, view []llm.Item, hash string) (string, error) {
	s.mu.Lock()
	text, ok := s.recorded[hash]
	s.mu.Unlock()
	if ok && s.Prompt == "" {
		s.count(SourceRecorded)

		return text, nil
	}
	file := s.file(hash)
	if file != "" {
		if data, err := os.ReadFile(file); err == nil {
			s.count(SourceCache)

			return string(data), nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("failed to read the summary cache: %w", err)
		}
	}
	if s.Live == nil {
		s.count(SourceStub)

		return StubSummary, nil
	}
	sum, err := s.Live(ctx, view)
	if err != nil {
		return "", err
	}
	if file != "" {
		if err := os.MkdirAll(s.Dir, 0o700); err != nil {
			return "", fmt.Errorf("failed to make the summary cache: %w", err)
		}
		if err := os.WriteFile(file, []byte(sum.Text), 0o600); err != nil {
			return "", fmt.Errorf("failed to write the summary cache: %w", err)
		}
	}
	s.count(SourceModel)

	return sum.Text, nil
}

func (s *Summaries) file(hash string) string {
	if s.Dir == "" {
		return ""
	}
	prompt := s.Prompt
	if prompt == "" {
		prompt = compaction.Prompt
	}
	key := sha256.Sum256([]byte(hash + "\x00" + prompt))

	return filepath.Join(s.Dir, hex.EncodeToString(key[:])+".txt")
}

func (s *Summaries) count(source string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Sources == nil {
		s.Sources = map[string]int{}
	}
	s.Sources[source]++
}

// Live is a Summarizer that asks model on the named provider with the
// prompt ("": Codex's), with the provider's own credentials, as a session's
// compaction does. The returned close releases the client.
func Live(provider, model string, effort llm.ReasoningEffort, prompt string, getenv func(string) string) (compaction.Summarizer, func() error, error) {
	for _, p := range embedded.DefaultProviders() {
		if p.Name != provider {
			continue
		}
		key := ""
		if p.APIKeyEnv != "" {
			key = getenv(p.APIKeyEnv)
		}
		client, err := p.NewClient(embedded.ClientConfig{APIKey: key, BaseURL: p.BaseURL, MaxAttempts: 3, Getenv: getenv})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to make the %s client: %w", provider, err)
		}
		call := compaction.SummaryCall{Adapter: client, Model: model, Effort: effort, Window: compaction.DefaultContextWindow, Prompt: prompt, CacheKey: "uah-compaction-eval"}

		return func(ctx context.Context, view []llm.Item) (compaction.Summary, error) {
			return compaction.Summarize(ctx, call, view) //nolint:wrapcheck // Summarize wraps its errors
		}, client.Close, nil
	}

	return nil, nil, fmt.Errorf("unknown provider %q", provider)
}

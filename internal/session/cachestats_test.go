package session_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"
	uharness "github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/internal/usage/cachestats"
)

func TestCacheRequests(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	sec := func(n int) time.Time { return at.Add(time.Duration(n) * time.Second) }
	tokens := func(in, cached int64) core.Tokens {
		return core.Tokens{InputTokens: in, CachedInputTokens: cached, OutputTokens: 100}
	}
	// The first run logged each attempt's effort, as adaptive effort does;
	// a retry's failed attempt and a direct call do not count.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, uharness.StderrFile), []byte(
		`{"diag":"model_attempt","at":"2026-10-03T09:00:00.05Z","kind":"turn","effort":"high","result":"ok"}
{"diag":"model_attempt","at":"2026-10-03T09:00:10.05Z","kind":"turn","effort":"low","result":"retry"}
{"diag":"model_attempt","at":"2026-10-03T09:00:11Z","kind":"turn","effort":"low","result":"ok"}
{"diag":"model_attempt","at":"2026-10-03T09:00:15Z","kind":"direct","effort":"xhigh","result":"ok"}
not json
`), 0o600))
	runs := []session.LoadedRun{
		{
			Record: uharness.RunRecord{Dir: dir, Result: core.Result{Request: core.Request{Model: "gpt-a", Effort: "high"}}},
			Events: []core.Event{
				core.UserMessage{At: sec(0), ID: "m1", Text: "fix it"},
				core.TurnStarted{At: sec(0), Turn: 1},
				core.ModelResponded{At: sec(9), Turn: 1, Usage: tokens(12_000, 4_096)},
				core.TurnStarted{At: sec(10), Turn: 2},
				core.ModelResponded{At: sec(19), Turn: 2, Usage: tokens(20_000, 4_096)},
				core.TurnStarted{At: sec(20), Turn: 3},
				core.ModelResponded{At: sec(21), Turn: 3, Failure: "server_error: down"}, // not billed
			},
		},
		{
			// A run from before the attempt lines named the effort: the
			// settings say it.
			Record: uharness.RunRecord{Dir: t.TempDir(), Result: core.Result{Request: core.Request{Model: "gpt-b", Effort: "medium"}}},
			Events: []core.Event{
				engine.Compacted{At: sec(100)},
				core.ControlInput{At: sec(110), Mode: "settings", Effort: "xhigh"},
				core.UserMessage{At: sec(110), ID: "m2", Text: "and the docs"},
				core.TurnStarted{At: sec(110), Turn: 1},
				core.ModelResponded{At: sec(120), Turn: 1, Usage: tokens(8_000, 3_968)},
				engine.Compacted{At: sec(125), Err: "failed"}, // the history stayed
				core.ModelResponded{At: sec(130), Turn: 2, Duration: 5 * time.Second, Usage: tokens(9_000, 7_936)},
			},
		},
	}

	got := session.CacheRequests(runs)

	want := []cachestats.Request{
		{Start: sec(0), End: sec(9), Model: "gpt-a", Effort: "high", Input: 12_000, Cached: 4_096, Output: 100, Opener: true},
		{Start: sec(10), End: sec(19), Model: "gpt-a", Effort: "low", Input: 20_000, Cached: 4_096, Output: 100},
		{Start: sec(110), End: sec(120), Model: "gpt-b", Effort: "xhigh", Input: 8_000, Cached: 3_968, Output: 100, Rewritten: true, Opener: true},
		{Start: sec(125), End: sec(130), Model: "gpt-b", Effort: "xhigh", Input: 9_000, Cached: 7_936, Output: 100},
	}
	assert.Equal(t, want, got)
}

// TestCacheRequests_EffortUpdates: with effort updates, the requests carry
// the session's base effort whatever effort an update set, so the cache's
// key is the request's effort and a switch costs no effort miss.
func TestCacheRequests_EffortUpdates(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	sec := func(n int) time.Time { return at.Add(time.Duration(n) * time.Second) }
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, uharness.StderrFile), []byte(
		`{"diag":"model_attempt","at":"2026-10-03T09:00:00.05Z","kind":"turn","effort":"high","request_effort":"high","result":"ok"}
{"diag":"model_attempt","at":"2026-10-03T09:00:10.05Z","kind":"turn","effort":"low","request_effort":"high","effort_update":true,"result":"ok"}
{"diag":"model_attempt","at":"2026-10-03T09:00:20.05Z","kind":"turn","effort":"high","request_effort":"high","effort_update":true,"result":"ok"}
`), 0o600))
	tokens := func(in, cached int64) core.Tokens {
		return core.Tokens{InputTokens: in, CachedInputTokens: cached, OutputTokens: 100}
	}
	runs := []session.LoadedRun{{
		Record: uharness.RunRecord{Dir: dir, Result: core.Result{Request: core.Request{Model: "gpt-a", Effort: "high"}}},
		Events: []core.Event{
			core.UserMessage{At: sec(0), ID: "m1", Text: "fix it"},
			core.TurnStarted{At: sec(0), Turn: 1},
			core.ModelResponded{At: sec(9), Turn: 1, Usage: tokens(12_000, 0)},
			core.TurnStarted{At: sec(10), Turn: 2},
			core.ModelResponded{At: sec(19), Turn: 2, Usage: tokens(13_000, 11_904)},
			core.TurnStarted{At: sec(20), Turn: 3},
			core.ModelResponded{At: sec(29), Turn: 3, Usage: tokens(14_000, 12_928)},
		},
	}}

	got := cachestats.Attribute(session.CacheRequests(runs), cachestats.TTL)

	require.Len(t, got, 3)
	for _, r := range got {
		assert.Equal(t, "high", r.Effort, "the request's effort keys the cache")
	}
	for _, r := range got[1:] {
		for _, m := range r.Misses {
			assert.NotEqual(t, cachestats.CauseEffort, m.Cause, "an update keeps the cache")
		}
	}
}

// TestCacheRequests_EffortUpdatesRejected: after the backend rejected the
// updates, the retry and the later requests carry their own effort, so a
// switch is an effort miss again.
func TestCacheRequests_EffortUpdatesRejected(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	sec := func(n int) time.Time { return at.Add(time.Duration(n) * time.Second) }
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, uharness.StderrFile), []byte(
		`{"diag":"model_attempt","at":"2026-10-03T09:00:00.05Z","kind":"turn","effort":"high","request_effort":"high","effort_update":true,"result":"failed"}
{"diag":"effort_updates","at":"2026-10-03T09:00:00.06Z","result":"off","error":"rejected"}
{"diag":"model_attempt","at":"2026-10-03T09:00:00.07Z","kind":"turn","effort":"high","result":"ok"}
{"diag":"model_attempt","at":"2026-10-03T09:00:10.05Z","kind":"turn","effort":"low","result":"ok"}
`), 0o600))
	tokens := func(in, cached int64) core.Tokens {
		return core.Tokens{InputTokens: in, CachedInputTokens: cached, OutputTokens: 100}
	}
	runs := []session.LoadedRun{{
		Record: uharness.RunRecord{Dir: dir, Result: core.Result{Request: core.Request{Model: "gpt-a", Effort: "high"}}},
		Events: []core.Event{
			core.UserMessage{At: sec(0), ID: "m1", Text: "fix it"},
			core.TurnStarted{At: sec(0), Turn: 1},
			core.ModelResponded{At: sec(9), Turn: 1, Usage: tokens(12_000, 0)},
			core.TurnStarted{At: sec(10), Turn: 2},
			core.ModelResponded{At: sec(19), Turn: 2, Usage: tokens(13_000, 0)},
		},
	}}

	got := cachestats.Attribute(session.CacheRequests(runs), cachestats.TTL)

	require.Len(t, got, 2)
	assert.Equal(t, "high", got[0].Effort)
	assert.Equal(t, "low", got[1].Effort, "the request's own effort")
	causes := []cachestats.Cause{}
	for _, m := range got[1].Misses {
		causes = append(causes, m.Cause)
	}
	assert.Contains(t, causes, cachestats.CauseEffort)
}

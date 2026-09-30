//go:build probe

package embedded_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/compaction"
	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/engine/embedded"
	"github.com/viktordanov/uah/internal/session"
)

// TestProbeRemoteCompactionEndToEnd runs a real session on openai-codex
// through the embedded engine: a command prints a code, /compact goes to
// the provider, and the next answer must still know the code, which only
// the tool output carried. Run it by hand (four small requests):
//
//	go test -tags probe -run TestProbeRemoteCompactionEndToEnd -v ./internal/engine/embedded/
//
// UAH_PROBE_MODEL overrides the model (default gpt-6.1-sol).
func TestProbeRemoteCompactionEndToEnd(t *testing.T) {
	model := os.Getenv("UAH_PROBE_MODEL")
	if model == "" {
		model = "gpt-6.1-sol"
	}
	state, workspace := t.TempDir(), t.TempDir()
	eng := embedded.New(embedded.Config{StateDir: state, Provider: "openai-codex", Getenv: os.Getenv, Compaction: compaction.Settings{Remote: true}})
	s, err := session.Open(context.Background(), eng, session.Options{Settings: session.Settings{
		Provider: "openai-codex", Model: model, Effort: "low", Workspace: workspace, MaxAttempts: 3,
	}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	ev := &events{t: t, s: s}
	ask(t, s, ev, "Run the shell command `echo PAPAYA-4217` and reply with just `done`.")
	require.NoError(t, s.Compact())
	ask(t, s, ev, "What did that command print? Reply with just its output.")

	var stats *compaction.Stats
	var answer string
	for _, x := range ev.all {
		switch v := x.(type) {
		case engine.Compacted:
			require.Empty(t, v.Err)
			stats = v.Stats
		case core.RunFinished:
			answer = v.Result.Answer
		}
	}
	require.NotNil(t, stats)
	t.Logf("strategy=%s phase=%s before=%d after=%d item≈%d call=%+v took=%s answer=%q",
		stats.Strategy, stats.Phase, stats.Before, stats.After, stats.SummaryTokens, stats.Call, stats.Took(), answer)
	assert.Equal(t, compaction.StrategyRemote, stats.Strategy)
	assert.Contains(t, answer, "PAPAYA-4217")
}

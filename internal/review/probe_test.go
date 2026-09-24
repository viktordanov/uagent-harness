//go:build probe

package review_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/unreallabsai/unreal-agent/harness/llm/clients/openaicodex"

	"github.com/viktordanov/uagent-harness/internal/engine/embedded"
	"github.com/viktordanov/uagent-harness/internal/review"
)

// TestProbe makes one real review through the openai-codex provider (the
// runner's client reads the Codex sign-in). Run it by hand:
//
//	go test -tags probe -run TestProbe -v ./internal/review/
//
// UAH_PROBE_MODEL overrides the model (default codex-auto-review).
func TestProbe(t *testing.T) {
	var provider embedded.Provider
	for _, p := range embedded.DefaultProviders() {
		if p.Name == review.CodexProvider {
			provider = p
		}
	}
	c, err := provider.NewClient(embedded.ClientConfig{BaseURL: openaicodex.BaseURL, MaxAttempts: 1, Getenv: os.Getenv})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	model := os.Getenv("UAH_PROBE_MODEL")
	if model == "" {
		model = review.CodexModel
	}
	r := review.New(c, review.Config{Model: model, Effort: review.DefaultEffort})
	req := review.Request{
		Action: review.Action{
			Tool: "Bash", Command: "go test ./...", Cwd: "/tmp/probe", SandboxMode: "workspace-write",
			SandboxPermissions: "require_escalated", Justification: "The sandbox blocks the Go build cache.",
		},
		UserMessages: []string{"Run the tests."},
	}

	start := time.Now()
	v, err := r.Review(context.Background(), req)
	require.NoError(t, err)
	t.Logf("model=%s latency=%s outcome=%s risk=%s failed=%v reason=%q", model, time.Since(start).Round(time.Millisecond), v.Outcome, v.Risk, v.Failed, v.Reason)
	t.Logf("usage: input=%d cached=%d output=%d reasoning=%d", v.Usage.InputTokens, v.Usage.CachedInputTokens, v.Usage.OutputTokens, v.Usage.ReasoningTokens)
}

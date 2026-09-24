package review_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/llm/clients/openai"

	"github.com/viktordanov/uagent-harness/internal/review"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

func client(t *testing.T, srv *fakellm.Server) llm.Adapter {
	t.Helper()
	attempts := 1
	c, err := openai.NewClient(openai.Config{APIKey: "test-key", BaseURL: srv.URL, MaxAttempts: &attempts})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	return c
}

func newReviewer(t *testing.T, replies ...fakellm.Reply) (*review.Reviewer, *fakellm.Server) {
	t.Helper()
	srv := fakellm.New(t, replies...)

	return review.New(client(t, srv), review.Config{Model: "codex-auto-review", Effort: llm.ReasoningEffortLow}), srv
}

func sample() review.Request {
	return review.Request{
		Action: review.Action{
			Tool: "Bash", Command: "git push origin feature", Cwd: "/ws", SandboxMode: "workspace-write",
			SandboxPermissions: "require_escalated", Justification: "Push the fix the user asked for.",
		},
		UserMessages: []string{"Fix the typo and push it to my feature branch."},
		RecentCalls:  []review.ToolCall{{Name: "Bash", Arguments: `{"command":"git commit -am typo"}`, Status: "exit 0"}},
		SessionID:    "s1",
	}
}

func TestReview_Allow(t *testing.T) {
	r, srv := newReviewer(t, fakellm.Reply{Text: `{"outcome":"allow"}`})

	v, err := r.Review(context.Background(), sample())

	require.NoError(t, err)
	assert.Equal(t, review.Allow, v.Outcome)
	assert.Equal(t, review.RiskLow, v.Risk)
	assert.False(t, v.Failed)
	assert.Equal(t, int64(100), v.Usage.InputTokens)
	reqs := srv.Requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "codex-auto-review", reqs[0].Model)
	assert.Equal(t, "low", reqs[0].Effort)
	assert.Empty(t, reqs[0].Tools)
	assert.Contains(t, reqs[0].System, "You are judging one planned coding-agent action.")
	require.Len(t, reqs[0].UserTexts, 1)
	assert.Contains(t, reqs[0].UserTexts[0], "git push origin feature")
}

func TestReview_Deny(t *testing.T) {
	r, _ := newReviewer(t, fakellm.Reply{
		Text: `{"risk_level":"critical","user_authorization":"unknown","outcome":"deny","rationale":"Sends ~/.ssh to an unknown host."}`,
	})

	v, err := r.Review(context.Background(), sample())

	require.NoError(t, err)
	assert.Equal(t, review.Verdict{
		Outcome: review.Deny, Risk: review.RiskCritical, Authorization: "unknown",
		Reason: "Sends ~/.ssh to an unknown host.", Usage: v.Usage,
	}, v)
}

func TestReview_MalformedAnswerFailsClosed(t *testing.T) {
	r, srv := newReviewer(t, fakellm.Reply{Text: "looks fine to me"}, fakellm.Reply{Text: `{"outcome":"maybe"}`}, fakellm.Reply{Text: `{"outcome":`})

	v, err := r.Review(context.Background(), sample())

	require.NoError(t, err)
	assert.Equal(t, review.Deny, v.Outcome)
	assert.True(t, v.Failed)
	assert.Contains(t, v.Reason, "auto-review failed")
	assert.Len(t, srv.Requests(), 3, "a bad answer is retried, as in Codex")
}

func TestReview_RetriesABadAnswer(t *testing.T) {
	r, srv := newReviewer(t, fakellm.Reply{Text: "allow"}, fakellm.Reply{Text: "```json\n{\"outcome\":\"allow\"}\n```"})

	v, err := r.Review(context.Background(), sample())

	require.NoError(t, err)
	assert.Equal(t, review.Allow, v.Outcome)
	assert.Len(t, srv.Requests(), 2)
	assert.Equal(t, int64(100+200), v.Usage.InputTokens, "usage adds up over attempts")
}

func TestReview_TimeoutFailsClosed(t *testing.T) {
	gate := make(chan struct{})
	t.Cleanup(func() { close(gate) })
	srv := fakellm.New(t, fakellm.Reply{Text: `{"outcome":"allow"}`, Gate: gate})
	r := review.New(client(t, srv), review.Config{Model: "m", Timeout: 200 * time.Millisecond})
	start := time.Now()

	v, err := r.Review(context.Background(), sample())

	require.NoError(t, err)
	assert.Less(t, time.Since(start), 10*time.Second)
	assert.Equal(t, review.Deny, v.Outcome)
	assert.True(t, v.Failed)
	assert.Contains(t, v.Reason, "did not finish before its deadline")
}

func TestReview_CancelledIsAnError(t *testing.T) {
	gate := make(chan struct{})
	t.Cleanup(func() { close(gate) })
	r, srv := newReviewer(t, fakellm.Reply{Text: `{"outcome":"allow"}`, Gate: gate})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-srv.Seen()
		cancel()
	}()

	_, err := r.Review(ctx, sample())

	require.ErrorIs(t, err, context.Canceled)
}

func TestReview_BreakerAsksTheUserAfterThreeDenials(t *testing.T) {
	deny := fakellm.Reply{Text: `{"outcome":"deny","risk_level":"high","rationale":"no"}`}
	r, srv := newReviewer(t, deny, fakellm.Reply{Text: `{"outcome":"allow"}`}, deny, deny, deny, fakellm.Reply{Text: `{"outcome":"allow"}`})
	ctx := context.Background()

	var got []review.Outcome
	for range 6 {
		v, err := r.Review(ctx, sample())
		require.NoError(t, err)
		got = append(got, v.Outcome)
	}

	assert.Equal(t, []review.Outcome{review.Deny, review.Allow, review.Deny, review.Deny, review.Deny, review.AskUser}, got)
	assert.Len(t, srv.Requests(), 5, "an open breaker does not call the model")

	r.Reset()
	v, err := r.Review(ctx, sample())
	require.NoError(t, err)
	assert.Equal(t, review.Allow, v.Outcome, "a new turn reviews again")
}

func TestBreaker_RecentWindow(t *testing.T) {
	var b review.Breaker
	for i := range review.MaxRecentDenials - 1 {
		assert.False(t, b.Record(true), "denial %d", i)
		assert.False(t, b.Record(false))
	}
	assert.True(t, b.Record(true), "10 denials within the last 50 reviews")
}

func TestParse(t *testing.T) {
	tests := []struct {
		name, text string
		want       review.Verdict
		err        string
	}{
		{name: "minimal allow", text: `{"outcome":"allow"}`, want: review.Verdict{Outcome: review.Allow, Risk: review.RiskLow, Authorization: "unknown", Reason: "Auto-review returned a low-risk allow decision."}},
		{name: "deny defaults to high", text: ` {"outcome":"deny"} `, want: review.Verdict{Outcome: review.Deny, Risk: review.RiskHigh, Authorization: "unknown", Reason: "Auto-review returned a deny decision without a rationale."}},
		{name: "fenced", text: "```json\n{\"outcome\":\"allow\",\"risk_level\":\"medium\",\"rationale\":\"ok\"}\n```", want: review.Verdict{Outcome: review.Allow, Risk: review.RiskMedium, Authorization: "unknown", Reason: "ok"}},
		{name: "prose", text: `Sure: {"outcome":"allow"}`, err: "failed to parse"},
		{name: "unknown field", text: `{"outcome":"allow","confidence":1}`, err: "failed to parse"},
		{name: "missing outcome", text: `{"risk_level":"low"}`, err: "invalid review outcome"},
		{name: "bad risk", text: `{"outcome":"allow","risk_level":"none"}`, err: "invalid review risk_level"},
		{name: "bad authorization", text: `{"outcome":"allow","user_authorization":"total"}`, err: "invalid review user_authorization"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := review.Parse(tt.text)
			if tt.err != "" {
				require.ErrorContains(t, err, tt.err)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultModel(t *testing.T) {
	assert.Equal(t, "codex-auto-review", review.DefaultModel("openai-codex", "gpt-6-sol"))
	assert.Equal(t, "gpt-6-astra", review.DefaultModel("openai", "gpt-6-astra"))
}

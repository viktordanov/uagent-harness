package embedded_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// openStreaming opens a session that asks for the model's text as it
// arrives, as the TUI does.
func (e *env) openStreaming(t *testing.T, eng engine.Engine) (*session.Session, *events) {
	t.Helper()
	s, err := session.Open(context.Background(), eng, session.Options{Settings: e.settings(), Stream: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	return s, &events{t: t, s: s}
}

// streamedText joins the text deltas in all.
func streamedText(all []core.Event) string {
	var b strings.Builder
	for _, x := range all {
		if d, ok := x.(engine.TextDelta); ok {
			b.WriteString(d.Text)
		}
	}

	return b.String()
}

// TestEmbedded_StreamsTheAnswer: the answer and the reasoning summary arrive
// as deltas while the response is still open, in order, and before the
// runner's final message, which they match.
func TestEmbedded_StreamsTheAnswer(t *testing.T) {
	t.Parallel()
	hold := make(chan struct{})
	e := newEnv(t, fakellm.Reply{Reasoning: []string{"Weighing ", "it"}, Deltas: []string{"Hello", ", ", "world"}, Hold: hold})
	s, ev := e.openStreaming(t, e.embedded())
	_, err := s.Submit("greet me")
	require.NoError(t, err)

	ev.until("the whole answer streamed", func(core.Event) bool { return streamedText(ev.all) == "Hello, world" })
	assert.Zero(t, countKind[core.AssistantMessage](ev.all), "the response is still open")
	close(hold)
	result := ev.finished()
	assert.Equal(t, "Hello, world", result.Answer)

	var order []string
	var reasoning strings.Builder
	for _, x := range ev.all {
		switch v := x.(type) {
		case engine.TextDelta:
			assert.Equal(t, "msg-1", v.ItemID)
			assert.True(t, v.Final, "the message is the final answer")
			order = append(order, "delta")
		case engine.ReasoningDelta:
			assert.Equal(t, "rs-1", v.ItemID)
			assert.Zero(t, v.Part)
			reasoning.WriteString(v.Text)
			order = append(order, "reasoning")
		case core.ReasoningSummary:
			assert.Equal(t, reasoning.String(), v.Text)
			order = append(order, "summary")
		case core.AssistantMessage:
			assert.Equal(t, "Hello, world", v.Text)
			order = append(order, "message")
		case engine.StreamReset:
			t.Error("nothing failed")
		}
	}
	assert.Equal(t, "Weighing it", reasoning.String())
	require.NotEmpty(t, order)
	assert.Equal(t, "reasoning", order[0])
	assert.Equal(t, []string{"summary", "message"}, order[len(order)-2:], "the final events come after every delta")
}

// TestEmbedded_StreamsOnlyTheTurnRequests: a session that does not ask
// gets no deltas, and a streaming session gets none from a compaction's
// summary call or the auto-reviewer's call.
func TestEmbedded_StreamsOnlyTheTurnRequests(t *testing.T) {
	t.Parallel()
	t.Run("not asked", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, fakellm.Reply{Deltas: []string{"quiet ", "answer"}})
		s, ev := e.open(t, e.embedded(), "")
		ask(t, s, ev, "hello")
		assert.Zero(t, countKind[engine.TextDelta](ev.all))
		assert.Equal(t, 1, countKind[core.AssistantMessage](ev.all))
	})
	t.Run("compaction", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, fakellm.Reply{Deltas: []string{"one"}}, fakellm.Reply{Deltas: []string{"SUMM", "ARY"}}, fakellm.Reply{Deltas: []string{"two"}})
		s, ev := e.openStreaming(t, e.compacting(0))
		ask(t, s, ev, "first")
		require.NoError(t, s.Compact())
		ask(t, s, ev, "second")
		var compacted engine.Compacted
		for _, x := range ev.all {
			if c, ok := x.(engine.Compacted); ok {
				compacted = c
			}
		}
		assert.Equal(t, "SUMMARY", compacted.Summary)
		assert.Equal(t, "onetwo", streamedText(ev.all), "the summary did not stream")
	})
	t.Run("auto-review", func(t *testing.T) {
		t.Parallel()
		verdict := `{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"The user asked for it."}`
		e := newApprovalEnv(t, approvalOpts{interactive: true, autoReview: true, stream: true}, func(outside string) []fakellm.Reply {
			return []fakellm.Reply{
				{Escalated: []string{"touch " + outside + "/x.txt"}},
				{Deltas: []string{verdict[:20], verdict[20:]}},
				{Deltas: []string{"done"}},
			}
		})
		e.run(t)
		assert.Equal(t, core.StatusOK, e.ev.finished().Status)
		assert.Equal(t, 1, countKind[engine.AutoReviewed](e.ev.all))
		assert.Equal(t, "done", streamedText(e.ev.all), "the review did not stream")
	})
}

// TestEmbedded_StreamResetsOnReconnect: a stream cut halfway is retried,
// and the text of the lost attempt is void before the next attempt's.
func TestEmbedded_StreamResetsOnReconnect(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the runner's backoff, about 2 s")
	}
	t.Parallel()
	e := newEnv(t, fakellm.Reply{Deltas: []string{"lost ", "words"}, Cut: true}, fakellm.Reply{Deltas: []string{"kept ", "words"}})
	s, ev := e.openStreaming(t, e.embedded())
	_, err := s.Submit("hello")
	require.NoError(t, err)
	assert.Equal(t, "kept words", ev.finished().Answer)

	var seq []string
	for _, x := range ev.all {
		switch v := x.(type) {
		case engine.TextDelta:
			seq = append(seq, v.Text)
		case engine.StreamReset:
			seq = append(seq, "RESET")
		case core.AssistantMessage:
			seq = append(seq, "FINAL "+v.Text)
		}
	}
	assert.Equal(t, "lost words|RESET|kept words|FINAL kept words", strings.Join(joinDeltas(seq), "|"))
}

// TestEmbedded_StreamResetsOnInterrupt: a request stopped while its answer
// streams records nothing, so its text is void.
func TestEmbedded_StreamResetsOnInterrupt(t *testing.T) {
	t.Parallel()
	hold := make(chan struct{})
	t.Cleanup(func() { close(hold) })
	e := newEnv(t, fakellm.Reply{Deltas: []string{"half an ", "answer"}, Hold: hold})
	s, ev := e.openStreaming(t, e.embedded())
	_, err := s.Submit("hello")
	require.NoError(t, err)
	ev.until("the answer streaming", func(core.Event) bool { return streamedText(ev.all) == "half an answer" })
	require.NoError(t, s.Interrupt())
	assert.Equal(t, core.StatusInterrupted, ev.finished().Status)
	assert.Equal(t, 1, countKind[engine.StreamReset](ev.all))
	assert.Zero(t, countKind[core.AssistantMessage](ev.all))
}

// joinDeltas joins neighboring deltas, which the stream may merge or not.
func joinDeltas(seq []string) []string {
	var out []string
	delta := false
	for _, s := range seq {
		isDelta := s != "RESET" && !strings.HasPrefix(s, "FINAL ")
		if isDelta && delta {
			out[len(out)-1] += s

			continue
		}
		out = append(out, s)
		delta = isDelta
	}

	return out
}

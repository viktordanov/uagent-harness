package embedded_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// openAttempts opens a session whose model requests get attempts tries
// (0: the engine's default).
func (e *env) openAttempts(t *testing.T, attempts int) (*session.Session, *events) {
	t.Helper()
	settings := e.settings()
	settings.MaxAttempts = attempts
	s, err := session.Open(context.Background(), e.embedded(), session.Options{Settings: settings})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	return s, &events{t: t, s: s}
}

// TestEmbedded_ReconnectsAfterALostConnection: a connection that drops
// before the answer, and one that drops halfway through it, are retried
// with the runner's backoff (2 s, then 4 s), each retry is reported, and
// the run then finishes as if nothing happened.
func TestEmbedded_ReconnectsAfterALostConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the runner's backoff, about 6 s")
	}
	t.Parallel()
	e := newEnv(t, fakellm.Reply{Drop: true}, fakellm.Reply{Cut: true, Text: "cut"}, fakellm.Reply{Text: "back"})
	s, ev := e.openAttempts(t, 0)
	_, err := s.Submit("hello")
	require.NoError(t, err)
	result := ev.finished()

	assert.Equal(t, core.StatusOK, result.Status)
	assert.Equal(t, "back", result.Answer)
	assert.Len(t, e.llm.Requests(), 3)
	var retries []engine.Reconnecting
	var ended []engine.ReconnectEnded
	for _, e := range ev.all {
		switch v := e.(type) {
		case engine.Reconnecting:
			retries = append(retries, v)
		case engine.ReconnectEnded:
			ended = append(ended, v)
		}
	}
	require.Len(t, retries, 2)
	assert.Equal(t, [2]int{2, engine.DefaultMaxAttempts}, [2]int{retries[0].Attempt, retries[0].MaxAttempts})
	assert.Equal(t, 2*time.Second, retries[0].Delay)
	assert.Equal(t, 3, retries[1].Attempt, "the stream cut halfway")
	assert.Equal(t, 4*time.Second, retries[1].Delay)
	assert.NotEmpty(t, retries[0].Reason)
	require.NotEmpty(t, ended)
	assert.True(t, ended[len(ended)-1].OK, "the answer arrived")
}

// TestEmbedded_GivesUpAfterMaxAttempts: when every attempt loses its
// connection, the run fails and says so in plain words.
func TestEmbedded_GivesUpAfterMaxAttempts(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the runner's backoff, about 2 s")
	}
	t.Parallel()
	e := newEnv(t, fakellm.Reply{Drop: true}, fakellm.Reply{Drop: true})
	s, ev := e.openAttempts(t, 2)
	_, err := s.Submit("hello")
	require.NoError(t, err)
	result := ev.finished()

	assert.Equal(t, core.StatusFailed, result.Status)
	assert.Len(t, e.llm.Requests(), 2)
	var message string
	var ended []engine.ReconnectEnded
	for _, e := range ev.all {
		switch v := e.(type) {
		case core.RunnerError:
			message = v.Message
		case engine.ReconnectEnded:
			ended = append(ended, v)
		}
	}
	t.Logf("the run's error: %s", message)
	assert.Contains(t, message, "gave up after 2 attempts because the connection to the model was lost")
	require.Len(t, ended, 1)
	assert.False(t, ended[0].OK)
}

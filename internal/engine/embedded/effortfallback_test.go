package embedded_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/engine/embedded"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/testing/fakellm"
)

// openOnUpdates opens the session id ("": a new one) on gpt-6.1-sol, which
// takes effort updates, at adaptive effort 2-steps.
func (e *env) openOnUpdates(t *testing.T, id string) (*session.Session, *events) {
	t.Helper()
	settings := e.settings()
	settings.Model, settings.AdaptiveEffort = "gpt-6.1-sol", session.AdaptiveTwoSteps
	eng := embedded.New(embedded.Config{StateDir: e.StateDir, Provider: "openai", Getenv: e.getenv, EffortUpdates: true})
	s, err := session.Open(t.Context(), eng, session.Options{ID: id, Resumed: id != "", Settings: settings})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	return s, &events{t: t, s: s}
}

// runTurn sends text and waits for the session to go idle.
func runTurn(t *testing.T, s *session.Session, ev *events, text string) core.Result {
	t.Helper()
	_, err := s.Submit(text)
	require.NoError(t, err)
	r := ev.finished()
	ev.idle()

	return r
}

// stderrLogs is every run's stderr.log, joined.
func (e *env) stderrLogs(t *testing.T) string {
	t.Helper()
	logs, err := filepath.Glob(filepath.Join(e.StateDir, "runs", "*", "stderr.log"))
	require.NoError(t, err)
	var all strings.Builder
	for _, l := range logs {
		all.WriteString(readFile(t, l))
	}

	return all.String()
}

func updatesOff(all []core.Event) []engine.EffortUpdatesOff {
	var off []engine.EffortUpdatesOff
	for _, e := range all {
		if o, ok := e.(engine.EffortUpdatesOff); ok {
			off = append(off, o)
		}
	}

	return off
}

// TestEffortUpdates_FallBack: a backend that rejects configuration_update
// items gets the request again without them, at the effort they set; the
// session then changes its effort per request, says so once, logs the
// error, and keeps them off when it resumes.
func TestEffortUpdates_FallBack(t *testing.T) {
	t.Parallel()
	e := newEnv(t, fakellm.Reply{Commands: []string{"echo one"}}, fakellm.Reply{Text: "done"}, fakellm.Reply{Text: "again"})
	e.llm.RejectEffortUpdates = true
	s, ev := e.openOnUpdates(t, "")

	r := runTurn(t, s, ev, "run it")

	assert.Equal(t, core.StatusOK, r.Status)
	reqs := e.llm.Requests()
	require.Len(t, reqs, 3)
	assert.Equal(t, []string{"high"}, reqs[0].EffortUpdates, "the baseline, rejected")
	assert.Nil(t, reqs[1].EffortUpdates, "the retry")
	assert.Equal(t, "high", reqs[1].Effort, "at the effort the update set")
	assert.Equal(t, reqs[0].UserTexts, reqs[1].UserTexts)
	assert.Nil(t, reqs[2].EffortUpdates)
	assert.Equal(t, "low", reqs[2].Effort, "adaptive effort's follow-up, as without updates")
	off := updatesOff(ev.all)
	require.Len(t, off, 1, "one notice")
	assert.Contains(t, off[0].Err, "configuration_update")
	assert.Equal(t, "effort updates were rejected by the backend; switching effort per request (cache misses on switches)", off[0].Text())
	logs := e.stderrLogs(t)
	assert.Contains(t, logs, `"diag":"effort_updates"`)
	assert.Contains(t, logs, `"result":"off","error":"create response: responses API error invalid_value: Invalid value: 'configuration_update'.`)
	assert.Contains(t, logs, `"effort":"high","effort_reason":"2-steps: first request","network_wait_ms"`, "the retry's line has no request_effort, so the cache accounting keys on its effort")
	id := s.ID()
	require.NoError(t, s.Close())
	assert.FileExists(t, filepath.Join(e.StateDir, "sessions", id+".effortupdates.json"))

	s, ev = e.openOnUpdates(t, id)
	runTurn(t, s, ev, "more")

	reqs = e.llm.Requests()
	require.Len(t, reqs, 4, "no rejected request after the resume")
	assert.Nil(t, reqs[3].EffortUpdates)
	assert.Equal(t, "high", reqs[3].Effort)
	assert.Empty(t, updatesOff(ev.all))
}

// TestEffortUpdates_FallBackOnAProbe: an invalid input that does not name
// the item turns the updates off only when the request without them
// succeeds; when it fails too, the first error stands and they stay on.
func TestEffortUpdates_FallBackOnAProbe(t *testing.T) {
	t.Parallel()
	invalidInput := fakellm.Reply{Fail: 400, FailBody: `{"error":{"message":"Invalid input.","type":"invalid_request_error","param":"input","code":"invalid_value"}}`}
	t.Run("the retry succeeds", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, invalidInput, fakellm.Reply{Text: "ok"}, fakellm.Reply{Text: "more"})
		s, ev := e.openOnUpdates(t, "")

		r := runTurn(t, s, ev, "hi")
		runTurn(t, s, ev, "again")

		assert.Equal(t, core.StatusOK, r.Status)
		reqs := e.llm.Requests()
		require.Len(t, reqs, 3)
		assert.NotEmpty(t, reqs[0].EffortUpdates)
		assert.Nil(t, reqs[1].EffortUpdates)
		assert.Nil(t, reqs[2].EffortUpdates)
		assert.Len(t, updatesOff(ev.all), 1)
	})
	t.Run("the retry fails too", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, invalidInput, invalidInput, fakellm.Reply{Text: "ok"})
		s, ev := e.openOnUpdates(t, "")

		r := runTurn(t, s, ev, "hi")
		runTurn(t, s, ev, "again")

		assert.NotEqual(t, core.StatusOK, r.Status)
		reqs := e.llm.Requests()
		require.Len(t, reqs, 3)
		assert.Nil(t, reqs[1].EffortUpdates, "the retry")
		assert.NotEmpty(t, reqs[2].EffortUpdates, "the next run's request keeps them")
		assert.Empty(t, updatesOff(ev.all))
		assert.Contains(t, e.stderrLogs(t), `"result":"kept"`)
		assert.NoFileExists(t, filepath.Join(e.StateDir, "sessions", s.ID()+".effortupdates.json"))
	})
}

// TestEffortUpdates_UnrelatedErrorKeepsThem: a 400 about something else,
// such as a tool, fails the request as before: no retry, and the next
// request still carries the updates.
func TestEffortUpdates_UnrelatedErrorKeepsThem(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"a tool":             `{"error":{"message":"Invalid 'tools[0].name': string too long.","type":"invalid_request_error","param":"tools[0].name","code":"string_above_max_length"}}`,
		"a context too long": `{"error":{"message":"Your input exceeds the context window of this model.","type":"invalid_request_error","param":"input","code":"context_length_exceeded"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, fakellm.Reply{Fail: 400, FailBody: body}, fakellm.Reply{Text: "ok"})
			s, ev := e.openOnUpdates(t, "")

			r := runTurn(t, s, ev, "hi")
			runTurn(t, s, ev, "again")

			assert.NotEqual(t, core.StatusOK, r.Status)
			reqs := e.llm.Requests()
			require.Len(t, reqs, 2, "no retry")
			assert.NotEmpty(t, reqs[1].EffortUpdates)
			assert.Empty(t, updatesOff(ev.all))
			assert.NotContains(t, e.stderrLogs(t), `"diag":"effort_updates"`)
		})
	}
}

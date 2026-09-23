package embedded_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"
	uaharness "github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/engine/embedded"
	"github.com/viktordanov/uagent-harness/internal/engine/process"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
	"github.com/viktordanov/uagent-harness/testing/harnesstest"
)

const waitTimeout = 20 * time.Second

type env struct {
	*harnesstest.Env
	llm *fakellm.Server
}

func newEnv(t *testing.T, replies ...fakellm.Reply) *env {
	t.Helper()

	return &env{Env: harnesstest.NewEnv(t), llm: fakellm.New(t, replies...)}
}

func (e *env) getenv(key string) string {
	switch key {
	case "OPENAI_API_KEY":
		return "test-key"
	case "SHELL":
		return "/bin/sh"
	}

	return e.Env.Getenv(key)
}

func (e *env) embedded() *embedded.Engine {
	return embedded.New(embedded.Config{StateDir: e.StateDir, Provider: "openai", Getenv: e.getenv})
}

func (e *env) settings() session.Settings {
	return session.Settings{Provider: "openai", Model: "gpt-test", Effort: "high", Workspace: e.Workspace, BaseURL: e.llm.URL}
}

// open opens a session and returns a reader of its events.
func (e *env) open(t *testing.T, eng engine.Engine, id string) (*session.Session, *events) {
	t.Helper()
	s, err := session.Open(context.Background(), eng, session.Options{ID: id, Resumed: id != "", Settings: e.settings()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	return s, &events{t: t, s: s}
}

type events struct {
	t   *testing.T
	s   *session.Session
	all []core.Event
}

// until reads events until match returns true and returns that event.
func (ev *events) until(what string, match func(core.Event) bool) core.Event {
	ev.t.Helper()
	deadline := time.After(waitTimeout)
	for {
		select {
		case e, ok := <-ev.s.Events():
			require.True(ev.t, ok, "the session closed while waiting for %s", what)
			ev.all = append(ev.all, e)
			if n, isNotice := e.(session.Notice); isNotice && n.Level == "error" {
				ev.t.Logf("notice: %s", n.Message)
			}
			if match(e) {
				return e
			}
		case <-deadline:
			ev.t.Fatalf("timed out waiting for %s; events: %v", what, kinds(ev.all))
		}
	}
}

func isA[T core.Event](e core.Event) bool { _, ok := e.(T); return ok }

func (ev *events) finished() core.Result {
	ev.t.Helper()

	return ev.until("RunFinished", isA[core.RunFinished]).(core.RunFinished).Result
}

func (ev *events) idle() { ev.t.Helper(); ev.until("Idle", isA[session.Idle]) }

func waitSeen(t *testing.T, s *fakellm.Server, n int) {
	t.Helper()
	deadline := time.After(waitTimeout)
	for {
		select {
		case got := <-s.Seen():
			if got >= n {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for model request %d", n)
		}
	}
}

func TestEmbedded_RunsToolsAndAnswers(t *testing.T) {
	e := newEnv(t,
		fakellm.Reply{Text: "Listing.", Commands: []string{"echo one", "echo two"}},
		fakellm.Reply{Text: "hello"},
	)
	s, ev := e.open(t, e.embedded(), "")
	assert.True(t, s.Capabilities().LiveInput)

	in, err := s.Submit("say hello")
	require.NoError(t, err)
	delivered := ev.until("delivery", isA[session.InputDelivered]).(session.InputDelivered)
	assert.Equal(t, in.ID, delivered.ID)
	result := ev.finished()
	ev.idle()

	assert.Equal(t, core.StatusOK, result.Status)
	assert.Equal(t, "hello", result.Answer)
	assert.Equal(t, 2, result.Stats.ToolCalls)
	assert.Equal(t, 0, result.Stats.FailedToolCalls)
	reqs := e.llm.Requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, "gpt-test", reqs[0].Model)
	assert.Equal(t, "high", reqs[0].Effort)
	assert.Equal(t, []string{"say hello"}, reqs[0].UserTexts)
	assert.Contains(t, reqs[0].System, "You are an AI agent", "the runner's host prompt")

	runDir := filepath.Join(e.StateDir, "runs", result.Request.RunID)
	for _, f := range []string{uaharness.RequestFile, uaharness.EventsFile, uaharness.SummaryFile} {
		assert.FileExists(t, filepath.Join(runDir, f))
	}
	assert.FileExists(t, filepath.Join(e.StateDir, "sessions", s.ID()+".session.jsonl"))
}

// TestEmbedded_MatchesTheRunner drives the real runner and the embedded engine
// with one script and compares what the harness sees.
func TestEmbedded_MatchesTheRunner(t *testing.T) {
	if testing.Short() {
		t.Skip("builds unreal-agent-runner")
	}
	script := func() []fakellm.Reply {
		return []fakellm.Reply{
			{Text: "Checking.", Commands: []string{"echo one", "false"}},
			{Text: "the answer"},
		}
	}
	runWith := func(t *testing.T, e *env, eng engine.Engine) (core.Result, []string, []string) {
		t.Helper()
		s, ev := e.open(t, eng, "")
		_, err := s.Submit("do the thing")
		require.NoError(t, err)
		result := ev.finished()
		ev.idle()
		items := sessionItemKinds(t, filepath.Join(e.StateDir, "sessions", s.ID()+".session.jsonl"))

		return result, summarize(ev.all), items
	}

	pe := newEnv(t, script()...)
	t.Setenv("OPENAI_API_KEY", "test-key")
	proc := process.New(uaharness.Config{RunnerPath: harnesstest.RealRunner(t), StateDir: pe.StateDir, Getenv: pe.getenv, KillGrace: time.Second})
	procResult, procEvents, procItems := runWith(t, pe, proc)

	ee := newEnv(t, script()...)
	embResult, embEvents, embItems := runWith(t, ee, ee.embedded())

	assert.Equal(t, procEvents, embEvents)
	assert.Equal(t, procItems, embItems, "the session files hold the same items")
	assert.Equal(t, procResult.Status, embResult.Status)
	assert.Equal(t, procResult.Answer, embResult.Answer)
	assert.Equal(t, procResult.Stats.ToolCalls, embResult.Stats.ToolCalls)
	assert.Equal(t, procResult.Stats.FailedToolCalls, embResult.Stats.FailedToolCalls)
	assert.Equal(t, procResult.Stats.Tokens, embResult.Stats.Tokens)
	assert.Equal(t, e2eRequests(pe.llm), e2eRequests(ee.llm), "the model sees the same requests")
}

func TestEmbedded_SteersALiveRun(t *testing.T) {
	gate := make(chan struct{})
	e := newEnv(t, fakellm.Reply{Text: "first answer", Gate: gate}, fakellm.Reply{Text: "second answer"})
	s, ev := e.open(t, e.embedded(), "")

	_, err := s.Submit("start")
	require.NoError(t, err)
	waitSeen(t, e.llm, 1)
	steer, err := s.SteerNow("also this")
	require.NoError(t, err)
	close(gate)

	delivered := ev.until("the steer's delivery", func(x core.Event) bool {
		d, ok := x.(session.InputDelivered)

		return ok && d.ID == steer.ID
	})
	assert.NotNil(t, delivered)
	result := ev.finished()
	ev.idle()

	assert.Equal(t, "second answer", result.Answer, "one run answered both messages")
	reqs := e.llm.Requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, []string{"start", "also this"}, reqs[1].UserTexts)
	assert.Equal(t, 1, countKind[core.RunStarted](ev.all), "the steer did not restart the run")
}

func TestEmbedded_ChangesSettingsLive(t *testing.T) {
	gate := make(chan struct{})
	e := newEnv(t, fakellm.Reply{Commands: []string{"true"}, Gate: gate}, fakellm.Reply{Text: "done"})
	s, ev := e.open(t, e.embedded(), "")
	assert.True(t, s.Capabilities().ServiceTier, "openai offers priority processing")

	_, err := s.Submit("go")
	require.NoError(t, err)
	waitSeen(t, e.llm, 1)
	next := e.settings()
	next.Effort, next.Model, next.ServiceTier = "low", "gpt-other", "priority"
	applied, err := s.SetSettings(next)
	require.NoError(t, err)
	assert.Equal(t, session.AppliedLive, applied)
	close(gate)
	ev.finished()

	reqs := e.llm.Requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, fakellm.Request{Model: "gpt-test", Effort: "high"}, trimmed(reqs[0]))
	assert.Equal(t, fakellm.Request{Model: "gpt-other", Effort: "low", ServiceTier: "priority"}, trimmed(reqs[1]))
}

func TestEmbedded_InterruptThenContinue(t *testing.T) {
	e := newEnv(t, fakellm.Reply{Commands: []string{"sleep 30"}})
	s, ev := e.open(t, e.embedded(), "")

	_, err := s.Submit("wait a while")
	require.NoError(t, err)
	ev.until("the tool starting", isA[core.ToolStarted])
	started := time.Now()
	require.NoError(t, s.Interrupt())
	result := ev.finished()
	assert.Equal(t, core.StatusInterrupted, result.Status)
	assert.Less(t, time.Since(started), 5*time.Second, "the hard stop cancels the tool")
	ev.idle()

	_, err = s.Submit("carry on")
	require.NoError(t, err)
	result = ev.finished()
	assert.Equal(t, core.StatusOK, result.Status)
	reqs := e.llm.Requests()
	assert.Equal(t, []string{"wait a while", "carry on"}, reqs[len(reqs)-1].UserTexts, "the session resumes after the hard stop")
}

func TestEmbedded_ResumesAProcessSession(t *testing.T) {
	if testing.Short() {
		t.Skip("builds unreal-agent-runner")
	}
	e := newEnv(t, fakellm.Reply{Text: "first"}, fakellm.Reply{Text: "second"})
	t.Setenv("OPENAI_API_KEY", "test-key")
	proc := process.New(uaharness.Config{RunnerPath: harnesstest.RealRunner(t), StateDir: e.StateDir, Getenv: e.getenv, KillGrace: time.Second})
	s, ev := e.open(t, proc, "")
	_, err := s.Submit("remember the number 7")
	require.NoError(t, err)
	ev.finished()
	ev.idle()
	id := s.ID()
	require.NoError(t, s.Close())

	s2, ev2 := e.open(t, e.embedded(), id)
	_, err = s2.Submit("what was the number?")
	require.NoError(t, err)
	result := ev2.finished()

	assert.Equal(t, "second", result.Answer)
	reqs := e.llm.Requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, []string{"remember the number 7", "what was the number?"}, reqs[1].UserTexts, "the embedded run replays the runner's history")
}

func trimmed(r fakellm.Request) fakellm.Request {
	return fakellm.Request{Model: r.Model, Effort: r.Effort, ServiceTier: r.ServiceTier}
}

func e2eRequests(s *fakellm.Server) []fakellm.Request {
	var out []fakellm.Request
	for _, r := range s.Requests() {
		r.System = ""
		out = append(out, r)
	}

	return out
}

func countKind[T core.Event](all []core.Event) int {
	n := 0
	for _, e := range all {
		if isA[T](e) {
			n++
		}
	}

	return n
}

// summarize reduces events to what must match across engines.
func summarize(all []core.Event) []string {
	var out []string
	for _, e := range all {
		switch v := e.(type) {
		case core.UserMessage:
			out = append(out, "user "+v.Text)
		case core.TurnStarted:
			out = append(out, fmt.Sprintf("turn %d", v.Turn))
		case core.ToolCalled:
			out = append(out, "call "+v.Name+" "+v.Label)
		case core.ToolFinished:
			out = append(out, fmt.Sprintf("done %s ok=%v", v.CallID, v.OK))
		case core.AssistantMessage:
			out = append(out, fmt.Sprintf("say %q final=%v", v.Text, v.Final))
		case core.RunnerError:
			out = append(out, "error "+v.Message)
		case core.RunFinished:
			out = append(out, "finished "+string(v.Result.Status))
		}
	}

	return out
}

func kinds(all []core.Event) []string {
	out := make([]string, 0, len(all))
	for _, e := range all {
		out = append(out, fmt.Sprintf("%T", e))
	}

	return out
}

// sessionItemKinds lists the "type" of each session file line.
func sessionItemKinds(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var out []string
	for line := range strings.Lines(string(data)) {
		i := strings.Index(line, `"Kind":"`)
		if i < 0 {
			i = strings.Index(line, `"type":"`)
		}
		if i < 0 {
			out = append(out, "?")

			continue
		}
		rest := line[i+8:]
		out = append(out, rest[:strings.IndexByte(rest, '"')])
	}

	return out
}

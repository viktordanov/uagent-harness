package embedded_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/approval"
	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/hooks"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/testing/fakellm"
)

// reviewDelay is how long each fake review takes.
const reviewDelay = 500 * time.Millisecond

// barrier holds each of n callers until all n have arrived, or until
// waitTimeout, and reports whether they all did.
type barrier struct {
	mu      sync.Mutex
	n       int
	arrived chan struct{}
}

func newBarrier(n int) *barrier { return &barrier{n: n, arrived: make(chan struct{})} }

func (b *barrier) wait() bool {
	b.mu.Lock()
	if b.n--; b.n == 0 {
		close(b.arrived)
	}
	b.mu.Unlock()
	select {
	case <-b.arrived:
		return true
	case <-time.After(waitTimeout / 2):
		return false
	}
}

// reviewer answers the auto-reviewer's requests: each waits for the others
// (a review that runs alone never gets its verdict) and takes reviewDelay,
// then gets the verdict for the file its action touches.
func reviewer(e *approvalEnv, n int, verdicts map[string]string) {
	b := newBarrier(n)
	from := func(req fakellm.Request) fakellm.Reply {
		if !b.wait() {
			return fakellm.Reply{Text: `{"outcome":"deny","rationale":"the reviews ran one at a time"}`}
		}
		time.Sleep(reviewDelay)
		_, action, _ := strings.Cut(strings.Join(req.UserTexts, "\n"), "Planned action JSON:")
		for file, verdict := range verdicts {
			if strings.Contains(action, file) {
				return fakellm.Reply{Text: verdict}
			}
		}

		return fakellm.Reply{Text: `{"outcome":"deny","rationale":"unknown action"}`}
	}
	replies := make([]fakellm.Reply, n)
	for i := range replies {
		replies[i] = fakellm.Reply{From: from}
	}
	e.llm.Route("APPROVAL REQUEST START", replies...)
}

const (
	allowVerdict = `{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"The user asked for it."}`
	denyVerdict  = `{"risk_level":"high","user_authorization":"unknown","outcome":"deny","rationale":"Writes outside the project."}`
)

// touches is a model that asks to create the files outside the sandbox in
// one response, then finishes.
func touches(files ...string) func(outside string) []fakellm.Reply {
	return func(outside string) []fakellm.Reply {
		var commands []string
		for _, f := range files {
			commands = append(commands, "touch "+filepath.Join(outside, f))
		}

		return []fakellm.Reply{{Escalated: commands}, {Text: "done"}}
	}
}

// requests waits for n approval requests, all open at once.
func (e *approvalEnv) requests(t *testing.T, n int) []session.ApprovalRequested {
	t.Helper()
	var reqs []session.ApprovalRequested
	for range n {
		reqs = append(reqs, e.ev.until("ApprovalRequested", isA[session.ApprovalRequested]).(session.ApprovalRequested))
	}

	return reqs
}

// TestEmbedded_ParallelAutoReviews pins that the reviews of one response's
// escalations run at once: each fake review waits for the other two, so
// one at a time they would never finish, and together they take about one
// review's time. Each call still gets its own verdict.
func TestEmbedded_ParallelAutoReviews(t *testing.T) {
	t.Parallel()
	e := newApprovalEnv(t, approvalOpts{interactive: true, mode: approval.ModeAuto}, touches("a.txt", "b.txt", "c.txt"))
	reviewer(e, 3, map[string]string{"a.txt": allowVerdict, "b.txt": denyVerdict, "c.txt": allowVerdict})
	e.run(t)

	assert.Equal(t, core.StatusOK, e.ev.finished().Status)
	var started, ended []time.Time
	for _, ev := range e.ev.all {
		switch v := ev.(type) {
		case engine.AutoReviewing:
			started = append(started, v.At)
		case engine.AutoReviewed:
			ended = append(ended, v.At)
		}
	}
	require.Len(t, started, 3)
	require.Len(t, ended, 3)
	took := ended[len(ended)-1].Sub(started[0])
	t.Logf("3 reviews of %v each took %v", reviewDelay, took)
	assert.Less(t, took, 3*reviewDelay, "the reviews overlapped")
	assert.Zero(t, countKind[session.ApprovalRequested](e.ev.all), "the user was not asked")
	assert.FileExists(t, filepath.Join(e.outside, "a.txt"))
	assert.NoFileExists(t, filepath.Join(e.outside, "b.txt"))
	assert.FileExists(t, filepath.Join(e.outside, "c.txt"))
	outputs := e.lastOutputs()
	assert.Equal(t, 1, strings.Count(outputs, "the auto-reviewer denied this (high risk): Writes outside the project."), outputs)
	assert.NotContains(t, outputs, "one at a time")
}

// TestEmbedded_ParallelPrompts pins that the user sees every prompt of a
// response at once, in the TUI's queue, and that each answer reaches its
// own call.
func TestEmbedded_ParallelPrompts(t *testing.T) {
	e := newApprovalEnv(t, approvalOpts{interactive: true}, touches("a.txt", "b.txt", "c.txt"))
	e.run(t)

	declined := ""
	for i, req := range e.requests(t, 3) {
		answer := approval.Approve
		if i == 1 {
			answer, declined = approval.Decline, req.Command
		}
		require.NoError(t, e.s.Resolve(req.ID, answer))
	}

	assert.Equal(t, core.StatusOK, e.ev.finished().Status)
	for _, f := range []string{"a.txt", "b.txt", "c.txt"} {
		_, err := os.Stat(filepath.Join(e.outside, f))
		assert.Equal(t, !strings.HasSuffix(declined, f), err == nil, f)
	}
	assert.Equal(t, 1, strings.Count(e.lastOutputs(), "the user declined this command"))
}

// TestEmbedded_ParallelDontAskAgain pins that "don't ask again" on one of
// the open prompts settles the others it covers, as it would one at a
// time: the second prompt resolves as approved without an answer.
func TestEmbedded_ParallelDontAskAgain(t *testing.T) {
	e := newApprovalEnv(t, approvalOpts{interactive: true}, touches("x.txt", "x.txt"))
	e.run(t)

	reqs := e.requests(t, 2)
	first, second := reqs[0], reqs[1]
	require.NoError(t, e.s.Resolve(first.ID, approval.ApprovePrefix))

	assert.Equal(t, core.StatusOK, e.ev.finished().Status)
	resolved := map[string]approval.Answer{}
	for _, ev := range e.ev.all {
		if r, ok := ev.(session.ApprovalResolved); ok {
			resolved[r.ID] = r.Decision
		}
	}
	assert.Equal(t, map[string]approval.Answer{first.ID: approval.ApprovePrefix, second.ID: approval.Approve}, resolved)
	assert.FileExists(t, filepath.Join(e.outside, "x.txt"))
	assert.NotContains(t, e.lastOutputs(), "declined")
}

// TestEmbedded_ParallelPreToolUseHooks pins that the PreToolUse hooks of
// one response's calls run at once, each before its call's approval, and
// that the approval sees the arguments a hook rewrote.
func TestEmbedded_ParallelPreToolUseHooks(t *testing.T) {
	marks := t.TempDir()
	hook := hooks.Hook{Event: hooks.PreToolUse, Matcher: "Bash", Source: hooks.SourceUser, Command: `
		input=$(cat)
		mktemp ` + marks + `/hook.XXXXXX >/dev/null
		i=0
		while [ "$(ls ` + marks + ` | wc -l)" -lt 3 ] && [ $i -lt 100 ]; do sleep 0.1; i=$((i+1)); done
		[ $i -lt 100 ] || { echo 'the hooks ran one at a time' >&2; exit 2; }
		case "$input" in
		*b.txt*) echo '{"hookSpecificOutput":{"updatedInput":{"command":"touch '"$OUTSIDE"'/hooked.txt","sandbox_permissions":"require_escalated","justification":"rewritten"}}}' ;;
		esac`}
	e := newApprovalEnv(t, approvalOpts{interactive: true, hooks: []hooks.Hook{hook}}, func(outside string) []fakellm.Reply {
		t.Setenv("OUTSIDE", outside)

		return touches("a.txt", "b.txt", "c.txt")(outside)
	})
	e.run(t)

	commands := map[string]bool{}
	for _, req := range e.requests(t, 3) {
		commands[filepath.Base(strings.TrimPrefix(req.Command, "touch "))] = true
		require.NoError(t, e.s.Resolve(req.ID, approval.Approve))
	}

	assert.Equal(t, core.StatusOK, e.ev.finished().Status)
	assert.Equal(t, map[string]bool{"a.txt": true, "hooked.txt": true, "c.txt": true}, commands, "the user was asked about the rewritten command")
	assert.FileExists(t, filepath.Join(e.outside, "hooked.txt"))
	assert.NoFileExists(t, filepath.Join(e.outside, "b.txt"))
	assert.NotContains(t, e.lastOutputs(), "blocked by a PreToolUse hook")
}

// TestEmbedded_InterruptStopsParallelReviews pins that an interrupt ends
// the reviews in progress at once: the coordinator waits for them before it
// reads the stop, and they would otherwise run to their timeout.
func TestEmbedded_InterruptStopsParallelReviews(t *testing.T) {
	never := make(chan struct{})
	t.Cleanup(func() { close(never) })
	e := newApprovalEnv(t, approvalOpts{interactive: true, mode: approval.ModeAuto}, touches("a.txt", "b.txt", "c.txt"))
	e.llm.Route("APPROVAL REQUEST START", fakellm.Reply{Gate: never}, fakellm.Reply{Gate: never}, fakellm.Reply{Gate: never})
	e.run(t)
	for range 3 {
		e.ev.until("AutoReviewing", isA[engine.AutoReviewing])
	}
	waitSeen(t, e.llm, 4) // the turn and the three reviews

	start := time.Now()
	require.NoError(t, e.s.Interrupt())

	result := e.ev.finished()
	assert.Less(t, time.Since(start), waitTimeout/4)
	assert.Equal(t, core.StatusInterrupted, result.Status)
	for _, f := range []string{"a.txt", "b.txt", "c.txt"} {
		assert.NoFileExists(t, filepath.Join(e.outside, f))
	}
}

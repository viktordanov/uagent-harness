package embedded

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/viktordanov/uah-core/harness/operation"
	"github.com/viktordanov/uah-core/harness/session"
	"github.com/viktordanov/uah-core/harness/sessionstore"

	"github.com/viktordanov/uagent/harness"
)

// failingStore refuses every operation write.
type failingStore struct{ sessionstore.Store }

func (failingStore) SaveOperation(context.Context, session.ID, operation.Operation) error {
	return errors.New("disk full")
}

type output struct{ bytes.Buffer }

func (*output) Close() error { return nil }

// finishRun finishes a run that ended with err and returns its exit code,
// output, and stderr.
func finishRun(t *testing.T, err error, interrupted bool, closers ...closer) (int, string, string) {
	t.Helper()
	var out output
	var stderr bytes.Buffer
	w := &wiring{l: harness.Launch{Stdout: &out, Stderr: &stderr}}
	a := &agent{done: make(chan struct{})}
	a.interrupted.Store(interrupted)
	w.finish(a, err, closers)

	return a.ExitCode(), out.String(), stderr.String()
}

// TestFinish_SaveFailureFailsTheRun: a checkpoint flush that fails at the
// run's end fails a run that succeeded, and the session gets the error.
func TestFinish_SaveFailureFailsTheRun(t *testing.T) {
	ck := &checkpointStore{Store: failingStore{}, id: "s", held: []operation.Operation{{ID: "a", Status: operation.StatusCompleted}}}
	var closed []string
	code, out, stderr := finishRun(t, nil, false,
		closer{close: func() error { closed = append(closed, "log"); return nil }, saves: true},
		closer{close: func() error { closed = append(closed, "flush"); return ck.flush(t.Context()) }, saves: true},
		closer{close: func() error { closed = append(closed, "client"); return nil }},
	)

	assert.Equal(t, 1, code)
	assert.JSONEq(t, `{"type":"error","message":"failed to save the session: disk full"}`, strings.TrimSpace(out))
	assert.Contains(t, stderr, "failed to save the session: disk full")
	assert.Equal(t, []string{"client", "flush", "log"}, closed, "every closer runs, in reverse")
}

// TestFinish_ReportsBothErrors: a run that failed and then could not close
// its session file reports both errors.
func TestFinish_ReportsBothErrors(t *testing.T) {
	code, out, _ := finishRun(t, errors.New("the coordinator stopped"), false,
		closer{close: func() error { return errors.New("sync failed") }, saves: true})

	assert.Equal(t, 1, code)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], "the coordinator stopped")
	assert.Contains(t, lines[1], "failed to save the session: sync failed")
}

// TestFinish_InterruptedKeepsItsCode: an interrupted run whose save fails
// still exits as interrupted, and the session gets the error.
func TestFinish_InterruptedKeepsItsCode(t *testing.T) {
	code, out, _ := finishRun(t, nil, true, closer{close: func() error { return errors.New("sync failed") }, saves: true})

	assert.Equal(t, exitInterrupted, code)
	assert.Contains(t, out, "failed to save the session: sync failed")
}

// TestFinish_OtherClosersDoNotFail: a closer that saves nothing keeps its
// error to itself, and a run whose closers succeed ends quietly.
func TestFinish_OtherClosersDoNotFail(t *testing.T) {
	code, out, stderr := finishRun(t, nil, false,
		closer{close: func() error { return nil }, saves: true},
		closer{close: func() error { return errors.New("connection reset") }})

	assert.Zero(t, code)
	assert.Empty(t, out)
	assert.Empty(t, stderr)
}

// TestCleanup_ReturnsSaveErrors: a failed start reports a failed save with
// its own error.
func TestCleanup_ReturnsSaveErrors(t *testing.T) {
	w := &wiring{closers: []closer{
		{close: func() error { return errors.New("sync failed") }, saves: true},
		{close: func() error { return errors.New("connection reset") }},
	}}

	require.EqualError(t, w.cleanup(), "failed to save the session: sync failed")
	assert.Nil(t, w.closers)
	assert.NoError(t, (&wiring{}).cleanup())
}

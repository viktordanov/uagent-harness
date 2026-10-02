package embedded

import (
	"context"
	"slices"
	"sync"

	"github.com/viktordanov/uah-core/harness/operation"
	"github.com/viktordanov/uah-core/harness/session"
	"github.com/viktordanov/uah-core/harness/sessionstore"
)

// checkpointStore writes fewer operation records: each is a line, one that
// is not terminal is a sync too (logStore), and a shell command's phases
// carry its output up to three times.
// See docs/design/state.md.
//
// It drops the shell phases a resume repeats harmlessly: the creates (a
// resume from ready creates the paths again, or uses them) and the reads
// after read_out (a resume from read_out reads both files again). It keeps
// process, the write-ahead point before the command starts, and read_out,
// which holds the exit code. Other versions of the shell pass through.
//
// It holds an operation's terminal state until the tool-call status that
// carries it is written, then writes it without the state, which the status
// has. The terminal line itself stays: localfile resumes operations only
// from operation lines, and the harness kills the process group of one
// whose last line is not terminal. Any other operation write writes the
// held states first, as does flush at the run's end. A stop while it holds
// one resumes from the record before, as a stop just before the terminal
// state would.
type checkpointStore struct {
	sessionstore.Store

	mu   sync.Mutex
	id   session.ID
	held []operation.Operation
}

// skippedShellPhases are the shell phases a resume repeats harmlessly
// (operation/shell.go, runner v0.2.0).
var skippedShellPhases = []operation.ShellPhase{
	operation.ShellPhaseCreateDirectory, operation.ShellPhaseCreateOut, operation.ShellPhaseCreateErr,
	operation.ShellPhaseReadOutTail, operation.ShellPhaseReadErr, operation.ShellPhaseReadErrTail,
}

func (c *checkpointStore) SaveOperation(ctx context.Context, id session.ID, v operation.Operation) error {
	if v.Type == operation.TypeShell && v.Status == operation.StatusAwaiting {
		if st, err := operation.DecodeShellState(v); err == nil && slices.Contains(skippedShellPhases, st.Phase) {
			return nil
		}
	}
	if !finalOperation(v.Status) {
		if err := c.flush(ctx); err != nil {
			return err
		}

		return c.Store.SaveOperation(ctx, id, v) //nolint:wrapcheck // the coordinator wraps store errors
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.id, c.held = id, append(c.held, v)

	return nil
}

func (c *checkpointStore) AppendToolCallStatus(ctx context.Context, id session.ID, s sessionstore.ToolCallStatus) error {
	c.mu.Lock()
	var carried []operation.Operation
	c.held = slices.DeleteFunc(c.held, func(h operation.Operation) bool {
		in := id == c.id && slices.ContainsFunc(s.Operations, func(op operation.Operation) bool { return op.ID == h.ID && op.Status == h.Status })
		if in {
			h.State = nil
			carried = append(carried, h)
		}

		return in
	})
	c.mu.Unlock()
	if err := c.flush(ctx); err != nil {
		return err
	}
	if err := c.Store.AppendToolCallStatus(ctx, id, s); err != nil {
		return err //nolint:wrapcheck // the coordinator wraps store errors
	}
	for _, op := range carried {
		if err := c.Store.SaveOperation(ctx, id, op); err != nil {
			return err //nolint:wrapcheck // as above
		}
	}

	return nil
}

// flush writes the held terminal states.
func (c *checkpointStore) flush(ctx context.Context) error {
	c.mu.Lock()
	held, id := c.held, c.id
	c.held = nil
	c.mu.Unlock()
	for _, op := range held {
		if err := c.Store.SaveOperation(ctx, id, op); err != nil {
			return err //nolint:wrapcheck // the coordinator wraps store errors
		}
	}

	return nil
}

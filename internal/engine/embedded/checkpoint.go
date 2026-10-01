package embedded

import (
	"context"
	"slices"

	"github.com/unreallabsai/unreal-agent/harness/operation"
	"github.com/unreallabsai/unreal-agent/harness/session"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore"
)

// checkpointStore writes fewer operation records: each is a line and an
// fsync, and a shell command's phases carry its output up to three times.
// See docs/design/state.md.
//
// It drops the shell phases a resume repeats harmlessly: the creates (a
// resume from ready creates the paths again, or uses them) and the reads
// after read_out (a resume from read_out reads both files again). It keeps
// process, the write-ahead point before the command starts, and read_out,
// which holds the exit code. Other versions of the shell pass through.
type checkpointStore struct {
	sessionstore.Store
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

	return c.Store.SaveOperation(ctx, id, v) //nolint:wrapcheck // the coordinator wraps store errors
}

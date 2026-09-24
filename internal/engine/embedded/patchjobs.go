package embedded

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/unreallabsai/unreal-agent/harness/operation"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/patch"
)

var errPatchInterrupted = errors.New("interrupted: the run stopped before this patch was applied; check the files before applying it again")

// patchJobs is the runner's RemoteJobHandler for apply_patch. A job reads
// the files, applies the patch, and completes with Codex's summary as its
// result and the diff as its handle.
type patchJobs struct {
	ctx     context.Context
	updates chan operation.Operation
}

func newPatchJobs(ctx context.Context) *patchJobs {
	return &patchJobs{ctx: ctx, updates: make(chan operation.Operation)}
}

func (*patchJobs) RemoteJobPlanType() operation.RemoteJobPlanType {
	return engine.PatchPlanType
}
func (*patchJobs) RemoteJobPlanVersion() operation.RemoteJobPlanVersion { return patchPlanVersion }
func (j *patchJobs) RemoteJobUpdates() <-chan operation.Operation       { return j.updates }

// AddRemoteJob applies a patch on its own goroutine. A job that had
// already started before a restart fails instead of applying twice.
func (j *patchJobs) AddRemoteJob(op operation.Operation) error {
	state, err := operation.DecodeRemoteJobState(op)
	if err != nil {
		return err //nolint:wrapcheck // the runner's own error
	}
	var plan patchPlan
	if err := json.Unmarshal(state.Plan.Data, &plan); err != nil {
		return err //nolint:wrapcheck // the operation manager reports it
	}
	if op.Status != operation.StatusReady {
		go j.finish(operation.FailRemoteJob(op, errPatchInterrupted))

		return nil
	}
	go j.run(op, state, plan)

	return nil
}

// CancelRemoteJob has nothing to stop: a patch applies at once.
func (*patchJobs) CancelRemoteJob(operation.ID, string) error { return nil }

func (j *patchJobs) run(op operation.Operation, state operation.RemoteJobState, plan patchPlan) {
	step, err := operation.UpdateRemoteJob(op, state, operation.StatusAwaiting)
	if err != nil || !j.send(*step.Operation) {
		return
	}
	op = *step.Operation
	changes, err := applyPatch(plan)
	if err != nil {
		j.finish(operation.FailRemoteJob(op, err))

		return
	}
	state.TerminalResult = patch.Summary(changes)
	state.Handle, _ = json.Marshal(engine.PatchHandle{Files: patch.Diffs(changes)}) //nolint:errchkjson // plain strings and ints always encode
	j.finish(operation.UpdateRemoteJob(op, state, operation.StatusCompleted))
}

// applyPatch computes the changes from the files as they are now and
// writes them.
func applyPatch(plan patchPlan) ([]patch.Change, error) {
	hunks, err := patch.Parse(plan.Patch)
	if err != nil {
		return nil, err //nolint:wrapcheck // Codex's message
	}
	changes, err := patch.Compute(plan.Cwd, hunks)
	if err != nil {
		return nil, err //nolint:wrapcheck // Codex's message
	}

	return changes, patch.Write(changes) //nolint:wrapcheck // Codex's message
}

func (j *patchJobs) finish(step operation.Step, err error) {
	if err == nil && step.Operation != nil {
		j.send(*step.Operation)
	}
}

func (j *patchJobs) send(op operation.Operation) bool {
	select {
	case j.updates <- op:
		return true
	case <-j.ctx.Done():
		return false
	}
}

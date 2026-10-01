package embedded

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/viktordanov/unreal-agent/harness/operation"

	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/patch"
)

var errPatchInterrupted = errors.New("interrupted: the run stopped before this patch was applied; check the files before applying it again")

// patchJobs is the runner's RemoteJobHandler for apply_patch. A job reads
// the files, applies the patch, and completes with Codex's summary as its
// result and the diff as its handle.
type patchJobs struct {
	ctx     context.Context
	updates chan operation.Operation
	// verify, when set, checks the projects a patch changed (autoverify.go);
	// checks are the running ones' cancels, by operation.
	verify *verifier
	mu     sync.Mutex
	checks map[operation.ID]context.CancelFunc
}

func newPatchJobs(ctx context.Context, verify *verifier) *patchJobs {
	return &patchJobs{ctx: ctx, updates: make(chan operation.Operation), verify: verify, checks: map[operation.ID]context.CancelFunc{}}
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

// CancelRemoteJob stops a patch's automatic check; a patch itself applies
// at once.
func (j *patchJobs) CancelRemoteJob(id operation.ID, _ string) error {
	j.mu.Lock()
	cancel := j.checks[id]
	j.mu.Unlock()
	if cancel != nil {
		cancel()
	}

	return nil
}

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
	state.TerminalResult = patch.Summary(changes) + j.check(op.ID, changes)
	state.Handle, _ = json.Marshal(engine.PatchHandle{Files: patch.Diffs(changes)}) //nolint:errchkjson // plain strings and ints always encode
	j.finish(operation.UpdateRemoteJob(op, state, operation.StatusCompleted))
}

// check runs the automatic check after a patch, if any, until it ends or
// the job is canceled.
func (j *patchJobs) check(id operation.ID, changes []patch.Change) string {
	if j.verify == nil {
		return ""
	}
	ctx, cancel := context.WithCancel(j.ctx)
	defer cancel()
	j.mu.Lock()
	j.checks[id] = cancel
	j.mu.Unlock()
	defer func() {
		j.mu.Lock()
		delete(j.checks, id)
		j.mu.Unlock()
	}()

	return j.verify.verify(ctx, changes)
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

package embedded

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/unreallabsai/unreal-agent/harness/operation"

	"github.com/viktordanov/uagent-harness/internal/engine"
)

var errAgentInterrupted = errors.New("interrupted: the run stopped while this agent call was running, and it was not repeated")

// agentJobs is the runner's RemoteJobHandler for the subagent tools. Each
// call runs on its own goroutine, so a wait never holds up the coordinator.
type agentJobs struct {
	ctx      context.Context
	agents   engine.Subagents // nil: subagents are off
	parentID string
	updates  chan operation.Operation

	mu      sync.Mutex
	cancels map[operation.ID]context.CancelFunc
}

func newAgentJobs(ctx context.Context, agents engine.Subagents, parentID string) *agentJobs {
	return &agentJobs{ctx: ctx, agents: agents, parentID: parentID, updates: make(chan operation.Operation), cancels: map[operation.ID]context.CancelFunc{}}
}

func (*agentJobs) RemoteJobPlanType() operation.RemoteJobPlanType       { return agentPlanType }
func (*agentJobs) RemoteJobPlanVersion() operation.RemoteJobPlanVersion { return agentPlanVersion }
func (j *agentJobs) RemoteJobUpdates() <-chan operation.Operation       { return j.updates }

// AddRemoteJob starts a call. A call that had already started before a
// restart fails instead of running twice.
func (j *agentJobs) AddRemoteJob(op operation.Operation) error {
	state, err := operation.DecodeRemoteJobState(op)
	if err != nil {
		return err //nolint:wrapcheck // the runner's own error
	}
	var plan agentPlan
	if err := json.Unmarshal(state.Plan.Data, &plan); err != nil {
		return err //nolint:wrapcheck // the operation manager reports it
	}
	if op.Status != operation.StatusReady {
		go j.finish(operation.FailRemoteJob(op, errAgentInterrupted))

		return nil
	}
	ctx, cancel := context.WithCancel(j.ctx)
	j.mu.Lock()
	j.cancels[op.ID] = cancel
	j.mu.Unlock()
	go j.run(ctx, op, state, plan)

	return nil
}

// CancelRemoteJob cancels a running call; it then reports canceled.
func (j *agentJobs) CancelRemoteJob(id operation.ID, _ string) error {
	j.mu.Lock()
	cancel := j.cancels[id]
	j.mu.Unlock()
	if cancel != nil {
		cancel()
	}

	return nil
}

func (j *agentJobs) run(ctx context.Context, op operation.Operation, state operation.RemoteJobState, plan agentPlan) {
	defer func() {
		j.mu.Lock()
		j.cancels[op.ID]()
		delete(j.cancels, op.ID)
		j.mu.Unlock()
	}()
	step, err := operation.UpdateRemoteJob(op, state, operation.StatusAwaiting)
	if err != nil || !j.send(*step.Operation) {
		return
	}
	op = *step.Operation
	out, err := j.call(ctx, plan)
	switch {
	case ctx.Err() != nil && j.ctx.Err() == nil: // canceled by the coordinator
		j.finish(operation.CancelRemoteJob(op))
	case err != nil:
		j.finish(operation.FailRemoteJob(op, err))
	default:
		state.TerminalResult = out
		j.finish(operation.UpdateRemoteJob(op, state, operation.StatusCompleted))
	}
}

func (j *agentJobs) call(ctx context.Context, plan agentPlan) (string, error) {
	if j.agents == nil {
		return "", errors.New("subagents are not enabled")
	}

	return j.agents.Call(ctx, j.parentID, plan.Tool, plan.Arguments) //nolint:wrapcheck // the subagents' errors are for the model
}

// finish sends a step's operation; a step that cannot be built was already
// validated when the job was added, so it is dropped.
func (j *agentJobs) finish(step operation.Step, err error) {
	if err == nil && step.Operation != nil {
		j.send(*step.Operation)
	}
}

func (j *agentJobs) send(op operation.Operation) bool {
	select {
	case j.updates <- op:
		return true
	case <-j.ctx.Done():
		return false
	}
}

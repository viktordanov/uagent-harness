package embedded

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/operation"

	"github.com/viktordanov/uagent-harness/internal/engine"
)

// wait's timeout bounds, Codex's.
const (
	waitDefault = 30 * time.Second
	waitMin     = 10 * time.Second
	waitMax     = time.Hour
)

var errAgentInterrupted = errors.New("interrupted: the run stopped while this agent call was running, and it was not repeated")

// agentJobs is the runner's RemoteJobHandler for the agent tools. Each call
// runs on its own goroutine, so a wait never holds up the coordinator.
type agentJobs struct {
	ctx      context.Context
	agents   engine.Subagents // nil: agents are off
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
	out, err := j.do(ctx, plan)
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

// Arguments of the agent tools.
type (
	spawnArgs struct {
		Message   string `json:"message"`
		AgentType string `json:"agent_type"`
		Model     string `json:"model"`
		Effort    string `json:"reasoning_effort"`
	}
	sendArgs struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	}
	waitArgs struct {
		IDs       []string `json:"ids"`
		TimeoutMS *float64 `json:"timeout_ms"`
	}
	closeArgs struct {
		ID string `json:"id"`
	}
)

// do runs one agent tool and returns its JSON result.
func (j *agentJobs) do(ctx context.Context, plan agentPlan) (string, error) {
	if j.agents == nil {
		return "", errors.New("subagents are not enabled")
	}
	var out any
	var err error
	switch plan.Tool {
	case toolSpawnAgent:
		out, err = decodeAnd(plan.Arguments, func(a spawnArgs) (any, error) {
			if strings.TrimSpace(a.Message) == "" {
				return nil, errors.New("the message is empty")
			}

			return j.agents.Spawn(ctx, j.parentID, engine.SpawnRequest{Message: a.Message, AgentType: a.AgentType, Model: a.Model, Effort: a.Effort})
		})
	case toolSendInput:
		out, err = decodeAnd(plan.Arguments, func(a sendArgs) (any, error) {
			if strings.TrimSpace(a.Message) == "" {
				return nil, errors.New("the message is empty")
			}

			return map[string]string{"id": a.ID, "status": "sent"}, j.agents.Send(j.parentID, a.ID, a.Message)
		})
	case toolWait:
		out, err = decodeAnd(plan.Arguments, func(a waitArgs) (any, error) { return j.wait(ctx, a) })
	case toolCloseAgent:
		out, err = decodeAnd(plan.Arguments, func(a closeArgs) (any, error) {
			prev, err := j.agents.CloseAgent(j.parentID, a.ID)

			return map[string]engine.AgentStatus{"previous_status": prev}, err
		})
	default:
		return "", fmt.Errorf("unknown agent tool %q", plan.Tool)
	}
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("failed to encode the result: %w", err)
	}

	return string(data), nil
}

func (j *agentJobs) wait(ctx context.Context, a waitArgs) (any, error) {
	if len(a.IDs) == 0 {
		return nil, errors.New("ids is empty")
	}
	timeout := waitDefault
	if a.TimeoutMS != nil {
		timeout = min(max(time.Duration(*a.TimeoutMS)*time.Millisecond, waitMin), waitMax)
	}
	statuses, timedOut, err := j.agents.Wait(ctx, j.parentID, a.IDs, timeout)
	if err != nil {
		return nil, err //nolint:wrapcheck // the manager's errors are for the model
	}

	return struct {
		Status   map[string]engine.AgentStatus `json:"status"`
		TimedOut bool                          `json:"timed_out"`
	}{statuses, timedOut}, nil
}

// decodeAnd decodes a tool's arguments and calls do with them.
func decodeAnd[A any](raw json.RawMessage, do func(A) (any, error)) (any, error) {
	var a A
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	return do(a)
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

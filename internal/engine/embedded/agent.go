package embedded

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/unreallabsai/unreal-agent/harness/inbox"
	"github.com/unreallabsai/unreal-agent/harness/llm"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/compaction"
)

// agent is one in-process coordinator run. It implements harness.Process;
// Send and the setters reach it while it runs.
type agent struct {
	ctx    context.Context
	cancel context.CancelFunc
	inputs *inbox.Inbox
	llm    *switcher
	// compactor and mode are set once the agent is wired.
	compactor *compactor
	mode      *modeCell

	interrupted atomic.Bool
	stopOnce    sync.Once
	done        chan struct{}
	code        int
}

func (a *agent) Done() <-chan struct{} { return a.done }

func (a *agent) ExitCode() int {
	<-a.done

	return a.code
}

// Interrupt submits a hard stop: the coordinator cancels the model call and
// the tools, records their state, and returns.
func (a *agent) Interrupt() {
	a.stopOnce.Do(func() {
		a.interrupted.Store(true)
		if a.compactor != nil {
			a.compactor.interrupt()
		}
		if err := a.control(inbox.ControlMessage{Mode: inbox.StopHard, Reason: "interrupted by the user"}); err != nil {
			a.cancel()
		}
	})
}

// Terminate and Kill cancel the coordinator; its tools stop with it.
func (a *agent) Terminate() { a.interrupted.Store(true); a.cancel() }
func (a *agent) Kill()      { a.Terminate() }

// Send delivers a message to the running agent.
func (a *agent) Send(in core.UserInput) error {
	input, err := messageInput(in)
	if err != nil {
		return err
	}

	return a.submit(input)
}

// messageInput is a user message as the inbox takes it.
func messageInput(in core.UserInput) (inbox.Input, error) {
	payload, err := json.Marshal(in.Text)
	if err != nil {
		return inbox.Input{}, fmt.Errorf("failed to encode message: %w", err)
	}

	return inbox.Input{ID: inbox.ID(in.ID), Kind: inbox.InputExternal, Payload: payload}, nil
}

// controlInput is a control message as the inbox takes it.
func controlInput(msg inbox.ControlMessage) (inbox.Input, error) {
	payload, err := json.Marshal(msg)
	if err != nil {
		return inbox.Input{}, fmt.Errorf("failed to encode control: %w", err)
	}

	return inbox.Input{ID: inbox.ID(uuid.NewString()), Kind: inbox.InputControl, Payload: payload}, nil
}

func (a *agent) SetEffort(effort string) error {
	return a.control(inbox.ControlMessage{Mode: inbox.UpdateSettings, Parameters: inbox.Settings{ReasoningEffort: reasoningEffort(effort)}})
}

func (a *agent) SetModel(model string) error {
	a.llm.setModel(model)

	return nil
}

func (a *agent) SetServiceTier(tier string) error { return a.llm.setPriority(tier == tierPriority) }

// Compact compacts the context before the next model request, the summary
// focused on focus when it is not empty.
func (a *agent) Compact(focus string) error {
	select {
	case <-a.done:
		return errStopped
	default:
	}
	a.compactor.requestCompaction(compaction.TriggerManual, focus)

	return nil
}

// Clear drops the context before the next model request (/clear).
func (a *agent) Clear() error {
	select {
	case <-a.done:
		return errStopped
	default:
	}
	a.compactor.requestCompaction(compaction.TriggerClear, "")

	return nil
}

func (a *agent) control(msg inbox.ControlMessage) error {
	input, err := controlInput(msg)
	if err != nil {
		return err
	}

	return a.submit(input)
}

func (a *agent) submit(in inbox.Input) error {
	select {
	case <-a.done:
		return errStopped
	default:
	}
	if err := a.inputs.Submit(a.ctx, in); err != nil {
		return fmt.Errorf("failed to reach the agent: %w", err)
	}

	return nil
}

// reasoningEffort maps a thinking level as the runner does: unknown levels are high.
func reasoningEffort(level string) llm.ReasoningEffort {
	switch level {
	case "low":
		return llm.ReasoningEffortLow
	case "medium":
		return llm.ReasoningEffortMedium
	case "xhigh":
		return llm.ReasoningEffortXHigh
	case "max":
		return llm.ReasoningEffortMax
	default:
		return llm.ReasoningEffortHigh
	}
}

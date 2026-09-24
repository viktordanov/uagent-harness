package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/viktordanov/uagent-harness/internal/engine"
)

// The tool names, Codex's v1 set.
const (
	ToolSpawn  = "spawn_agent"
	ToolSend   = "send_input"
	ToolWait   = "wait_agent"
	ToolClose  = "close_agent"
	ToolResume = "resume_agent"
	// toolWaitBefore is what uah called wait_agent before; the name still
	// resolves, so a session with past calls resumes, but it is not offered.
	toolWaitBefore = "wait"
)

// wait_agent's timeout bounds, Codex's.
const (
	waitDefault = 30 * time.Second
	waitMin     = 10 * time.Second
	waitMax     = time.Hour
)

// tool is one subagent tool: what the model is offered and how a call runs.
// A new tool is one more entry in tools.
type tool struct {
	name        string
	description func(m *Manager) string
	schema      string
	run         func(ctx context.Context, m *Manager, parentID string, args json.RawMessage) (any, error)
}

var tools = []tool{
	{ToolSpawn, func(m *Manager) string { return spawnDescription(m.cfg.Roles) }, spawnSchema, runSpawn},
	{ToolSend, fixed(sendDescription), sendSchema, runSend},
	{ToolWait, fixed(waitDescription), waitSchema, runWait},
	{ToolClose, fixed(closeDescription), closeSchema, runClose},
	{ToolResume, fixed(resumeDescription), resumeSchema, runResume},
}

func fixed(s string) func(*Manager) string { return func(*Manager) string { return s } }

// ToolNames are the tools' names and the names past calls may use.
func (m *Manager) ToolNames() []string {
	names := []string{toolWaitBefore}
	for _, t := range tools {
		names = append(names, t.name)
	}

	return names
}

// definitions are the tools as the engine offers them.
func (m *Manager) definitions() []engine.AgentTool {
	out := make([]engine.AgentTool, 0, len(tools))
	for _, t := range tools {
		var params map[string]any
		if err := json.Unmarshal([]byte(t.schema), &params); err != nil {
			panic(fmt.Sprintf("invalid %s schema: %v", t.name, err)) // a constant
		}
		out = append(out, engine.AgentTool{Name: t.name, Description: t.description(m), Parameters: params})
	}

	return out
}

// Call runs one tool call and returns its JSON result.
func (m *Manager) Call(ctx context.Context, parentID, name string, args json.RawMessage) (string, error) {
	for _, t := range tools {
		if t.name != name {
			continue
		}
		out, err := t.run(ctx, m, parentID, args)
		if err != nil {
			return "", err
		}
		data, err := json.Marshal(out)
		if err != nil {
			return "", fmt.Errorf("failed to encode the result: %w", err)
		}

		return string(data), nil
	}

	return "", fmt.Errorf("unknown agent tool %q", name)
}

// Arguments and results of the tools.
type (
	spawnArgs struct {
		Message   string `json:"message"`
		AgentType string `json:"agent_type"`
		Model     string `json:"model"`
		Effort    string `json:"reasoning_effort"`
	}
	spawnResult struct {
		AgentID  string `json:"agent_id"`
		Nickname string `json:"nickname"`
	}
	sendArgs struct {
		Target    string `json:"target"`
		Message   string `json:"message"`
		Interrupt bool   `json:"interrupt"`
	}
	sendResult struct {
		SubmissionID string `json:"submission_id"`
	}
	waitArgs struct {
		Targets   []string `json:"targets"`
		TimeoutMS *float64 `json:"timeout_ms"`
	}
	waitResult struct {
		Status   map[string]Status `json:"status"`
		TimedOut bool              `json:"timed_out"`
	}
	closeArgs struct {
		Target string `json:"target"`
	}
	closeResult struct {
		PreviousStatus Status `json:"previous_status"`
	}
	resumeArgs struct {
		ID string `json:"id"`
	}
	resumeResult struct {
		Status Status `json:"status"`
	}
)

func runSpawn(ctx context.Context, m *Manager, parentID string, raw json.RawMessage) (any, error) {
	a, err := decode[spawnArgs](raw)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.Message) == "" {
		return nil, errEmptyMessage
	}

	return m.spawn(ctx, parentID, a)
}

func runSend(_ context.Context, m *Manager, parentID string, raw json.RawMessage) (any, error) {
	a, err := decode[sendArgs](raw)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.Message) == "" {
		return nil, errEmptyMessage
	}
	id, err := m.send(parentID, a.Target, a.Message, a.Interrupt)

	return sendResult{SubmissionID: id}, err
}

func runWait(ctx context.Context, m *Manager, parentID string, raw json.RawMessage) (any, error) {
	a, err := decode[waitArgs](raw)
	if err != nil {
		return nil, err
	}
	if len(a.Targets) == 0 {
		return nil, errors.New("agent ids must be non-empty")
	}
	timeout := waitDefault
	if a.TimeoutMS != nil {
		if *a.TimeoutMS <= 0 {
			return nil, errors.New("timeout_ms must be greater than zero")
		}
		timeout = min(max(time.Duration(*a.TimeoutMS)*time.Millisecond, waitMin), waitMax)
	}
	statuses, timedOut, err := m.wait(ctx, parentID, a.Targets, timeout)

	return waitResult{Status: bound(statuses), TimedOut: timedOut}, err
}

func runClose(_ context.Context, m *Manager, parentID string, raw json.RawMessage) (any, error) {
	a, err := decode[closeArgs](raw)
	if err != nil {
		return nil, err
	}
	prev, err := m.closeAgent(parentID, a.Target)

	return closeResult{PreviousStatus: prev}, err
}

func runResume(ctx context.Context, m *Manager, parentID string, raw json.RawMessage) (any, error) {
	a, err := decode[resumeArgs](raw)
	if err != nil {
		return nil, err
	}
	status, err := m.resume(ctx, parentID, strings.TrimSpace(a.ID))

	return resumeResult{Status: status}, err
}

var errEmptyMessage = errors.New("empty message can't be sent to an agent")

// decode decodes a tool's arguments.
func decode[A any](raw json.RawMessage) (A, error) {
	var a A
	if err := json.Unmarshal(raw, &a); err != nil {
		return a, fmt.Errorf("invalid arguments: %w", err)
	}

	return a, nil
}

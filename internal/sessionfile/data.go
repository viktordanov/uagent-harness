package sessionfile

import (
	"encoding/json"
	"fmt"
)

// Input is an input item: a message or a control input, by Kind.
type Input struct {
	ID   string
	Kind string // external, control, or crash
	// Payload is a JSON string for an external input (the message text)
	// and an object for a control input.
	Payload json.RawMessage
}

// The input kinds.
const (
	InputExternal = "external"
	InputControl  = "control"
	InputCrash    = "crash"
)

// Text is an external input's message.
func (in Input) Text() (string, error) {
	var text string
	if err := json.Unmarshal(in.Payload, &text); err != nil {
		return "", fmt.Errorf("failed to decode input %s: %w", in.ID, err)
	}

	return text, nil
}

// Control is a control input's payload: Mode is hard, when_idle,
// heartbeat, or settings.
type Control struct {
	Mode       string
	Reason     string
	Parameters json.RawMessage
}

// Turn is a turn item: one model request and the tool calls it makes.
type Turn struct {
	ID             string
	PreviousTurnID string
	Type           string // regular or compaction
}

// ModelResponse is a model_response item: the model's answer in a turn.
type ModelResponse struct {
	TurnID   string
	Response Response
}

// Response is what the model returned.
type Response struct {
	ID      string
	Stop    string // complete, max_output_tokens, or refused
	Output  []Output
	Usage   Usage
	Failure *Failure
}

// Output is one output of a response; Data decodes by Type.
type Output struct {
	ProviderID string
	Type       string // message, reasoning, or tool_call
	Data       json.RawMessage
}

// The output types.
const (
	OutputMessage   = "message"
	OutputReasoning = "reasoning"
	OutputToolCall  = "tool_call"
)

// Decode decodes the output's data into v: a Message, Reasoning, or ToolCall.
func (o Output) Decode(v any) error {
	if err := json.Unmarshal(o.Data, v); err != nil {
		return fmt.Errorf("failed to decode a %s output: %w", o.Type, err)
	}

	return nil
}

// Message is a message output. Phase final_answer marks the answer.
type Message struct {
	Role  string
	Text  string
	Phase string
}

// Reasoning is a reasoning output's summary.
type Reasoning struct {
	Summary []string
}

// ToolCall is a tool_call output; Arguments are JSON text.
type ToolCall struct {
	CallID    string
	Name      string
	Arguments string
}

// Usage is the response's tokens. InputTokens includes the cached ones and
// OutputTokens the reasoning ones.
type Usage struct {
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
	ReasoningTokens   int64
}

// Failure is why a response failed.
type Failure struct {
	Code    string
	Message string
}

// ToolCallStatus is a tool_call_status item: a call's state, in its turn.
type ToolCallStatus struct {
	TurnID string
	CallID string
	Status struct {
		Error      string
		WaitingFor []string // the operations the call waits for
	}
}

// Fork is a fork item: the items before it came from ParentID's history
// up to PreviousTurnID.
type Fork struct {
	ParentID       string
	PreviousTurnID string
}

// Operation is the part of an operation snapshot a reader of tool output
// needs. Result is set once the operation finished; OutPath and ErrPath
// hold the full output under sessions/operations/<id>/.
type Operation struct {
	ID     string
	Type   string
	Status string
	State  struct {
		Result  json.RawMessage
		OutPath string
		ErrPath string
	}
}

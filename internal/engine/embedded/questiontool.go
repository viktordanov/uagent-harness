package embedded

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/viktordanov/uah-core/harness/llm"
	"github.com/viktordanov/uah-core/harness/operation"
	"github.com/viktordanov/uah-core/harness/tool"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/session"
)

// request_user_input is Codex's blocking question tool (codex-rs
// core/src/tools/handlers/request_user_input.rs and _spec.rs): the agent
// asks the user one to three questions with a few options each and waits,
// with no time limit, for the answers, which return as the call's result.
// The wait is the call's decision (gatedTranslator), as an approval's is, so
// the coordinator's loop waits with it and neither the heartbeat nor the
// wake valve reaches the model meanwhile. An answered call then runs as a
// remote job that completes at once with the answers, which the session
// file keeps with the questions. See docs/design/questions.md.

const questionPlanVersion operation.RemoteJobPlanVersion = 1

// Codex's texts, word for word.
const (
	questionDescription = "Request user input for one to three short questions and wait for the response."
	questionRootOnly    = "request_user_input can only be used by the root thread"
	questionCanceled    = "request_user_input was cancelled before receiving a response"
	questionNoOptions   = "request_user_input requires non-empty options for every question"
)

// questionNoUser refuses a call no one can answer: the session's only reader
// is a script.
const questionNoUser = "request_user_input is not available: no user can answer in this session; ask in your final message instead"

// questionSchema is Codex's schema for the tool, word for word, with its
// keys in the order Codex's BTreeMap sends them.
const questionSchema = `{"type":"object","properties":{"questions":{"type":"array",` +
	`"description":"Questions to show the user. Prefer 1 and do not exceed 3",` +
	`"items":{"type":"object","properties":{` +
	`"header":{"type":"string","description":"Short header label shown in the UI (12 or fewer chars)."},` +
	`"id":{"type":"string","description":"Stable identifier for mapping answers (snake_case)."},` +
	`"options":{"type":"array","description":"Provide 2-3 mutually exclusive choices. Put the recommended option first and suffix its label with \"(Recommended)\". ` +
	`Do not include an \"Other\" option in this list; the client will add a free-form \"Other\" option automatically.",` +
	`"items":{"type":"object","properties":{` +
	`"description":{"type":"string","description":"One short sentence explaining impact/tradeoff if selected."},` +
	`"label":{"type":"string","description":"User-facing label (1-5 words)."}},` +
	`"required":["label","description"],"additionalProperties":false}},` +
	`"question":{"type":"string","description":"Single-sentence prompt shown to the user."}},` +
	`"required":["id","header","question","options"],"additionalProperties":false}}},` +
	`"required":["questions"],"additionalProperties":false}`

// questionParameters is questionSchema as the runner's tool takes it.
var questionParameters = func() map[string]any {
	var params map[string]any
	if err := json.Unmarshal([]byte(questionSchema), &params); err != nil {
		panic(fmt.Sprintf("invalid %s schema: %v", engine.QuestionToolName, err)) // a constant
	}

	return params
}()

// questionRegistry offers request_user_input, and resolves its name even
// when it is not offered, so a session with past calls resumes anywhere.
type questionRegistry struct {
	tool.Registry

	t questionTranslator
}

func withQuestions(r tool.Registry, t questionTranslator) tool.Registry {
	return questionRegistry{Registry: r, t: t}
}

func (r questionRegistry) StaticDefinitions() []tool.Definition {
	defs := r.Registry.StaticDefinitions()
	if r.t.offered {
		defs = append(defs, tool.Definition{Tool: llm.Tool{
			Type: llm.ToolFunction, Name: engine.QuestionToolName, Description: questionDescription, Parameters: questionParameters,
		}})
	}

	return defs
}

func (r questionRegistry) Resolve(name string) (tool.Translator, bool) {
	if t, ok := r.Registry.Resolve(name); ok || name != engine.QuestionToolName {
		return t, ok
	}

	return r.t, true
}

// offersQuestions reports whether the run's session gets the tool: with
// Config.AskUser (a session a user drives), in the main agent's session,
// and in a child forked from it (Forker), which keeps its parent's tools
// so its requests keep the parent's prompt cache, and is refused at the
// call, as Codex refuses every thread but the root. A scope or the
// request can leave it out.
func (w *wiring) offersQuestions(req core.Request) bool {
	if !w.e.cfg.AskUser || slices.Contains(req.DisallowedTools, engine.QuestionToolName) {
		return false
	}
	if !isSubagent(req.SessionID) {
		return true
	}
	f, ok := w.e.cfg.Subagents.(interface{ Forked(sessionID string) bool })

	return ok && f.Forked(req.SessionID)
}

func isSubagent(sessionID string) bool { return strings.HasPrefix(sessionID, session.SubagentIDPrefix) }

// questionTranslator asks the user, then submits the answers as a job.
type questionTranslator struct {
	offered bool
	// root is the main agent's session; only it may ask.
	root bool
	// ctx bounds a wait decided in Translate: the run's approvals, ended
	// early by an interrupt.
	ctx context.Context
	ask engine.AskUser
}

func (t questionTranslator) Translate(ctx tool.Context, call llm.ToolCall) tool.CallStatus {
	return t.decide(t.ctx, call)(ctx)
}

// decide checks the questions and waits for the user's answers under ctx.
func (t questionTranslator) decide(ctx context.Context, call llm.ToolCall) submit {
	switch {
	case !t.offered:
		return refuse(tool.ErrorStatus(fmt.Sprintf("tool %q is not available in this session", engine.QuestionToolName), 0))
	case !t.root:
		return refuse(tool.ErrorStatus(questionRootOnly, 0))
	case t.ask == nil:
		return refuse(tool.ErrorStatus(questionNoUser, 0))
	}
	questions, err := parseQuestions(call.Arguments)
	if err != nil {
		return refuse(tool.ErrorStatus(err.Error(), 0))
	}
	answers, err := t.ask(ctx, engine.QuestionRequest{CallID: call.CallID, Questions: questions})
	if err != nil {
		return refuse(tool.ErrorStatus(questionCanceled, 0))
	}
	data, err := json.Marshal(engine.QuestionPlan{Questions: questions, Answers: answers})
	if err != nil {
		return refuse(tool.ErrorStatus(fmt.Sprintf("failed to encode the answers: %v", err), 0))
	}
	spec, err := operation.NewRemoteJobSpec(operation.RemoteJobPlan{Type: engine.QuestionPlanType, Version: questionPlanVersion, Data: jsontext.Value(data)})
	if err != nil {
		return refuse(tool.ErrorStatus(fmt.Sprintf("failed to build the answers' job: %v", err), 0))
	}

	return func(tc tool.Context) tool.CallStatus {
		return tool.CallStatus{WaitingFor: []operation.ID{tc.Submit(spec)}}
	}
}

// parseQuestions reads the call's questions. Codex checks only that every
// question has options; uah also needs an ID per question, to map the
// answers, and at least one question.
func parseQuestions(arguments string) ([]engine.Question, error) {
	var args struct {
		Questions []engine.Question `json:"questions"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse function arguments: %w", err)
	}
	if len(args.Questions) == 0 {
		return nil, errors.New("request_user_input requires at least one question")
	}
	seen := map[string]bool{}
	for _, q := range args.Questions {
		if len(q.Options) == 0 {
			return nil, errors.New(questionNoOptions)
		}
		if q.ID == "" || seen[q.ID] {
			return nil, errors.New("request_user_input requires a unique id for every question")
		}
		seen[q.ID] = true
	}

	return args.Questions, nil
}

func (questionTranslator) TranslateResult(callID string, status tool.CallStatus, ops []operation.Operation) (llm.ToolResult, error) {
	result := llm.ToolResult{CallID: callID}
	text := func(s string) {
		result.Output = append(result.Output, llm.ToolResultOutput{Kind: llm.ToolResultText, Value: s})
	}
	if status.Error != "" {
		text(status.Error)

		return result, nil
	}
	if len(ops) != 1 {
		return result, fmt.Errorf("request_user_input call %q has %d operations, want 1", callID, len(ops))
	}
	state, err := operation.DecodeRemoteJobState(ops[0])
	if err != nil {
		return result, fmt.Errorf("failed to decode request_user_input call %q: %w", callID, err)
	}
	switch ops[0].Status {
	case operation.StatusReady, operation.StatusAwaiting, operation.StatusCanceling:
		text("The answers are still being recorded.")
	case operation.StatusFailed:
		text(state.TerminalError)
	case operation.StatusCanceled:
		text(questionCanceled)
	case operation.StatusCompleted:
		text(state.TerminalResult)
	}

	return result, nil
}

// questionJobs is the runner's RemoteJobHandler for answered questions. A
// job completes at once with the answers in Codex's encoding; it has no
// side effect, so a job a restart left unfinished completes the same way.
type questionJobs struct {
	ctx     context.Context
	updates chan operation.Operation
}

func newQuestionJobs(ctx context.Context) *questionJobs {
	return &questionJobs{ctx: ctx, updates: make(chan operation.Operation)}
}

func (*questionJobs) RemoteJobPlanType() operation.RemoteJobPlanType {
	return engine.QuestionPlanType
}

func (*questionJobs) RemoteJobPlanVersion() operation.RemoteJobPlanVersion {
	return questionPlanVersion
}
func (j *questionJobs) RemoteJobUpdates() <-chan operation.Operation { return j.updates }

// CancelRemoteJob has nothing to stop: a job completes at once.
func (*questionJobs) CancelRemoteJob(operation.ID, string) error { return nil }

func (j *questionJobs) AddRemoteJob(op operation.Operation) error {
	state, err := operation.DecodeRemoteJobState(op)
	if err != nil {
		return err // the runner's own error
	}
	var plan engine.QuestionPlan
	if err := json.Unmarshal(state.Plan.Data, &plan); err != nil {
		return err // the operation manager reports it
	}
	go j.run(op, state, plan)

	return nil
}

func (j *questionJobs) run(op operation.Operation, state operation.RemoteJobState, plan engine.QuestionPlan) {
	if op.Status == operation.StatusReady {
		step, err := operation.UpdateRemoteJob(op, state, operation.StatusAwaiting)
		if err != nil || !j.send(*step.Operation) {
			return
		}
		op = *step.Operation
	}
	state.TerminalResult = engine.QuestionResult(plan.Answers)
	step, err := operation.UpdateRemoteJob(op, state, operation.StatusCompleted)
	if err == nil && step.Operation != nil {
		j.send(*step.Operation)
	}
}

func (j *questionJobs) send(op operation.Operation) bool {
	select {
	case j.updates <- op:
		return true
	case <-j.ctx.Done():
		return false
	}
}

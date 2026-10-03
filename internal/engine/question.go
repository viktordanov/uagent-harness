package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"time"
)

// QuestionToolName is Codex's question tool: the agent asks the user one to
// three short questions, each with a few options, and waits for the
// answers (codex-rs core/src/tools/handlers/request_user_input_spec.rs).
const QuestionToolName = "request_user_input"

// QuestionPlanType is the remote job plan an answered request_user_input
// call runs as. The plan keeps the questions and the answers, and the
// completed job returns the answers to the model.
const QuestionPlanType = "uah.request_user_input"

// OtherAnswer is the label of the choice the client adds to every
// question, as Codex's TUI adds it ("None of the above"): the user answers
// in their own words, in a note.
const OtherAnswer = "None of the above"

// NotePrefix starts an answer the user typed, as Codex's TUI sends a note.
const NotePrefix = "user_note: "

// QuestionOption is one choice of a question. Preview is uah's addition
// to Codex's schema: an optional monospace text (code, an ASCII layout, a
// config) the picker shows beside the options.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
	Preview     string `json:"preview,omitempty"`
}

// Question is one of a request_user_input call's questions.
type Question struct {
	ID       string           `json:"id"`
	Header   string           `json:"header"`
	Question string           `json:"question"`
	Options  []QuestionOption `json:"options"`
}

// Answer is the user's answer to one question: the chosen option's label,
// then a note they typed (NotePrefix), as Codex's RequestUserInputAnswer.
// An empty list is a question left unanswered.
type Answer struct {
	Answers []string `json:"answers"`
}

// Answers are the answers by question ID.
type Answers map[string]Answer

// QuestionRequest is a request_user_input call: its ID and its questions.
type QuestionRequest struct {
	CallID    string
	Questions []Question
}

// AskUser shows the questions to the user and waits for the answers, with
// no time limit. It returns ctx's error when ctx ends first, such as when
// the user interrupts the run.
type AskUser func(ctx context.Context, q QuestionRequest) (Answers, error)

// QuestionResult is the tool's result for the model: Codex's
// RequestUserInputResponse, {"answers":{"<id>":{"answers":[...]}}}.
func QuestionResult(a Answers) string {
	if a == nil {
		a = Answers{}
	}
	out, _ := json.Marshal(struct { //nolint:errchkjson // strings only
		Answers Answers `json:"answers"`
	}{a})

	return string(out)
}

// QuestionPlan is the plan of an answered call's job.
type QuestionPlan struct {
	Questions []Question `json:"questions"`
	Answers   Answers    `json:"answers"`
}

// QuestionsAnswered is what the user answered to a request_user_input
// call. The embedded engine adds it to the run's stream when the session
// stores the call's result, and session.Load adds it to a loaded
// transcript, from the same stored result.
type QuestionsAnswered struct {
	At        time.Time
	CallID    string
	Questions []Question
	Answers   Answers
}

func (e QuestionsAnswered) OccurredAt() time.Time { return e.At }

// QuestionsFromItem reads a session item, one line of the session file as
// the runner writes it, and returns the answers when the item completes a
// request_user_input call.
func QuestionsFromItem(line []byte) (QuestionsAnswered, bool) {
	if !bytes.Contains(line, []byte(QuestionPlanType)) {
		return QuestionsAnswered{}, false
	}
	var item struct {
		RecordedAt time.Time
		Kind       string
		Data       struct {
			CallID     string
			Operations []struct {
				Type, Status string
				State        struct {
					Plan struct {
						Type string
						Data json.RawMessage
					}
				}
			}
		}
	}
	if json.Unmarshal(line, &item) != nil || item.Kind != toolCallStatus {
		return QuestionsAnswered{}, false
	}
	for _, op := range item.Data.Operations {
		if op.Type != remoteJob || op.Status != opCompleted || op.State.Plan.Type != QuestionPlanType {
			continue
		}
		var p QuestionPlan
		if json.Unmarshal(op.State.Plan.Data, &p) == nil {
			return QuestionsAnswered{At: item.RecordedAt, CallID: item.Data.CallID, Questions: p.Questions, Answers: p.Answers}, true
		}
	}

	return QuestionsAnswered{}, false
}

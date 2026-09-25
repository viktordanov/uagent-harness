package fakellm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// A reply with Deltas or Reasoning streams them as the Responses API does:
// output_item.added opens an item, its text arrives in delta events, and
// response.completed carries the whole response, which the runner keeps.

// text is the message: Text, or the deltas joined.
func (r Reply) text() string {
	if r.Text == "" {
		return strings.Join(r.Deltas, "")
	}

	return r.Text
}

// phase is the message's phase: the final answer unless it goes with
// tool calls.
func (r Reply) phase() string {
	if len(r.Commands)+len(r.Escalated)+len(r.Calls) > 0 {
		return "commentary"
	}

	return "final_answer"
}

// deltaEvent is a stream event before the response completes; only
// reasoning deltas mean their summary_index.
type deltaEvent struct {
	Type         string      `json:"type"`
	OutputIndex  int         `json:"output_index"`
	ItemID       string      `json:"item_id,omitempty"`
	SummaryIndex int         `json:"summary_index"`
	Delta        string      `json:"delta,omitempty"`
	Item         *outputItem `json:"item,omitempty"`
}

const typeMessage = "message"

func messageID(n int) string   { return fmt.Sprintf("msg-%d", n) }
func reasoningID(n int) string { return fmt.Sprintf("rs-%d", n) }

func reasoningItem(n int, reply Reply) outputItem {
	return outputItem{
		ID: reasoningID(n), Type: "reasoning", Status: completed,
		Summary: []contentPart{{Type: "summary_text", Text: strings.Join(reply.Reasoning, "")}},
	}
}

// streamPieces writes the reply's reasoning and message pieces, then waits
// for Hold. It reports false when the request was canceled while held.
func streamPieces(w http.ResponseWriter, r *http.Request, n int, reply Reply) bool {
	if len(reply.Deltas) == 0 && len(reply.Reasoning) == 0 {
		return true
	}
	rc := http.NewResponseController(w)
	send := func(event deltaEvent) {
		data, err := json.Marshal(event)
		if err != nil {
			panic(err) // a struct of strings and numbers always encodes
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		_ = rc.Flush()
	}
	index := 0
	if len(reply.Reasoning) > 0 {
		send(deltaEvent{Type: "response.output_item.added", OutputIndex: index, Item: &outputItem{ID: reasoningID(n), Type: "reasoning"}})
		for _, piece := range reply.Reasoning {
			pace(reply.Pace)
			send(deltaEvent{Type: "response.reasoning_summary_text.delta", OutputIndex: index, ItemID: reasoningID(n), Delta: piece})
		}
		index++
	}
	if len(reply.Deltas) > 0 {
		send(deltaEvent{Type: "response.output_item.added", OutputIndex: index, Item: &outputItem{ID: messageID(n), Type: typeMessage, Role: "assistant", Phase: reply.phase()}})
		for _, piece := range reply.Deltas {
			pace(reply.Pace)
			send(deltaEvent{Type: "response.output_text.delta", OutputIndex: index, ItemID: messageID(n), Delta: piece})
		}
	}
	if reply.Hold != nil {
		select {
		case <-reply.Hold:
		case <-r.Context().Done():
			return false
		}
	}

	return true
}

func pace(d time.Duration) {
	if d > 0 {
		time.Sleep(d)
	}
}

package fakellm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// A reply with Deltas, Reasoning, or Searches streams them as the
// Responses API does: output_item.added opens an item, its text arrives in
// delta events, output_item.done closes a web search, and
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

const (
	typeMessage    = "message"
	eventItemAdded = "response.output_item.added"
)

func messageID(n int) string   { return fmt.Sprintf("msg-%d", n) }
func reasoningID(n int) string { return fmt.Sprintf("rs-%d", n) }

// searchItem is search i of response n as a finished web_search_call.
func searchItem(n, i int, search Search) outputItem {
	action := search.Action
	if action == "" {
		action = "search"
	}

	return outputItem{
		ID: fmt.Sprintf("ws-%d-%d", n, i), Type: "web_search_call", Status: completed,
		Action: &searchAction{Type: action, Query: search.Query, URL: search.URL, Pattern: search.Pattern},
	}
}

func reasoningItem(n int, reply Reply) outputItem {
	return outputItem{
		ID: reasoningID(n), Type: "reasoning", Status: completed,
		Summary: []contentPart{{Type: "summary_text", Text: strings.Join(reply.Reasoning, "")}},
	}
}

// streamPieces writes the reply's reasoning and message pieces, then waits
// for Hold. It reports false when the request was canceled while held.
func streamPieces(w http.ResponseWriter, r *http.Request, n int, reply Reply) bool {
	if len(reply.Deltas) == 0 && len(reply.Reasoning) == 0 && len(reply.Searches) == 0 && len(reply.ArgDeltas) == 0 {
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
	for index, item := range response(n, reply).Output {
		switch item.Type {
		case "web_search_call":
			started := outputItem{ID: item.ID, Type: item.Type, Status: "in_progress"}
			send(deltaEvent{Type: eventItemAdded, OutputIndex: index, Item: &started})
			pace(reply.Pace)
			send(deltaEvent{Type: "response.output_item.done", OutputIndex: index, Item: &item})
		case "reasoning":
			send(deltaEvent{Type: eventItemAdded, OutputIndex: index, Item: &outputItem{ID: item.ID, Type: item.Type}})
			for _, piece := range reply.Reasoning {
				pace(reply.Pace)
				send(deltaEvent{Type: "response.reasoning_summary_text.delta", OutputIndex: index, ItemID: item.ID, Delta: piece})
			}
		case typeMessage:
			if len(reply.Deltas) == 0 && len(reply.Searches) == 0 {
				continue
			}
			send(deltaEvent{Type: eventItemAdded, OutputIndex: index, Item: &outputItem{ID: item.ID, Type: item.Type, Role: item.Role, Phase: item.Phase}})
			for _, piece := range reply.Deltas {
				pace(reply.Pace)
				send(deltaEvent{Type: "response.output_text.delta", OutputIndex: index, ItemID: item.ID, Delta: piece})
			}
		default: // a function call opens when searches precede it or its arguments stream
			if len(reply.Searches)+len(reply.ArgDeltas) > 0 {
				send(deltaEvent{Type: eventItemAdded, OutputIndex: index, Item: &outputItem{ID: item.ID, Type: item.Type, CallID: item.CallID, Name: item.Name}})
			}
			for _, piece := range reply.ArgDeltas {
				pace(reply.Pace)
				send(deltaEvent{Type: "response.function_call_arguments.delta", OutputIndex: index, ItemID: item.ID, Delta: piece})
			}
			if len(reply.ArgDeltas) > 0 {
				send(deltaEvent{Type: "response.output_item.done", OutputIndex: index, Item: &item})
			}
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

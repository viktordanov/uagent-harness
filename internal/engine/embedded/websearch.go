package embedded

import (
	"bytes"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/llm"

	"github.com/viktordanov/uagent-harness/internal/engine"
)

// Web search is the provider's hosted tool (docs/design/web-search.md).
// The runner encodes llm.Tool{Type: ToolHosted, Name: "web_search"} as
// {"type":"web_search"} (responsesapi/request.go in v0.1.1) and drops the
// web_search_call items of a response, so the model's searches leave no
// record. The tee reads them from the stream instead, for the transcript.

// webSearchCall is the output item type of a hosted web search.
const webSearchCall = "web_search_call"

var webSearchTool = llm.Tool{Type: llm.ToolHosted, Name: "web_search"}

// hostedTools are the hosted tools a run offers: web search when the
// engine offers it and the run's provider has it.
func (w *wiring) hostedTools(provider string) []llm.Tool {
	if !w.e.cfg.WebSearch {
		return nil
	}
	if p, err := w.e.provider(provider); err != nil || !p.WebSearch {
		return nil
	}

	return []llm.Tool{webSearchTool}
}

// webSearchAction is a web_search_call item's action. A search has a
// query, or queries in newer responses.
type webSearchAction struct {
	Type    string   `json:"type"`
	Query   string   `json:"query"`
	Queries []string `json:"queries"`
	URL     string   `json:"url"`
	Pattern string   `json:"pattern"`
}

// query is the search's query, or its first query with " ..." when there
// are more, as Codex shows it.
func (a webSearchAction) query() string {
	switch {
	case a.Query != "":
		return a.Query
	case len(a.Queries) == 1:
		return a.Queries[0]
	case len(a.Queries) > 1 && a.Queries[0] != "":
		return a.Queries[0] + " ..."
	}

	return ""
}

var webSearchType = []byte(`"` + webSearchCall + `"`)

// wants reports whether the tee decodes a stream line: a web search item,
// or with text an event other than a finished item. Finished items are
// large and only a web search's matters.
func (s *stream) wants(payload []byte) bool {
	if bytes.Contains(payload, webSearchType) {
		return true
	}

	return s.text && !bytes.Contains(payload, []byte(`"response.output_item.done"`))
}

// search reports a web search item that started or finished.
func (s *stream) search(ev streamEvent) {
	var e engine.WebSearch
	switch ev.Type {
	case "response.output_item.added":
		e = engine.WebSearch{At: time.Now(), ItemID: ev.Item.ID}
	case "response.output_item.done":
		a := ev.Item.Action
		e = engine.WebSearch{At: time.Now(), ItemID: ev.Item.ID, Done: true, Action: a.Type, Query: a.query(), URL: a.URL, Pattern: a.Pattern}
	default:
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.pending = append(s.pending, e)
		s.wakeLocked()
	}
}

package embedded

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"maps"
	"net/http"
	"slices"
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

// searchLog opens the session's recorded searches on a provider that runs
// web search, so its turn requests get them back; nil elsewhere.
func (w *wiring) searchLog(provider, sessionID string) (*searchLog, error) {
	if p, err := w.e.provider(provider); err != nil || !p.WebSearch {
		return nil, nil //nolint:nilnil // no log: the provider has no web search
	}
	l, err := openSearchLog(w.l.SessionsDir, sessionID)
	if l != nil {
		l.logger = w.e.cfg.Logger
	}

	return l, err
}

// withRecordedSearches puts the session's recorded searches back into a
// turn request's body. Other requests, such as a compaction summary or an
// auto-review, carry no stream and pass unchanged.
func withRecordedSearches(req *http.Request) (*http.Request, error) {
	st, _ := req.Context().Value(streamKey{}).(*stream)
	if st == nil || st.log == nil {
		return req, nil
	}

	return st.log.withSearches(req)
}

// webSearchAction is a web_search_call item's action. A search has a
// query, or queries in newer responses.
type webSearchAction struct {
	Type    string   `json:"type"`
	Query   string   `json:"query,omitempty"`
	Queries []string `json:"queries,omitempty"`
	URL     string   `json:"url,omitempty"`
	Pattern string   `json:"pattern,omitempty"`
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
// an item that starts when searches are recorded (for their anchors), or
// with text an event other than a finished item. Finished items are large
// and only a web search's matters.
func (s *stream) wants(payload []byte) bool {
	switch {
	case bytes.Contains(payload, webSearchType):
		return true
	case s.log != nil && bytes.Contains(payload, []byte(`"response.output_item.added"`)):
		return true
	}

	return s.text && !bytes.Contains(payload, []byte(`"response.output_item.done"`))
}

// attemptOutputs are an attempt's output items by output index, and its
// finished web searches, from which record makes the searchRecords.
type attemptOutputs struct {
	items    map[int]outputRef
	searches map[int]json.RawMessage
}

// outputRef is an output item's type and ID.
type outputRef struct{ typ, id string }

// output notes an output item that started or a web search that finished.
func (s *stream) output(ev streamEvent) {
	if s.log == nil || ev.OutputIndex == nil || ev.Item.Type == "" {
		return
	}
	var item json.RawMessage
	if ev.Type == "response.output_item.done" && ev.Item.Type == webSearchCall {
		item = searchItem(ev)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	o := &s.outputs
	if o.items == nil {
		o.items, o.searches = map[int]outputRef{}, map[int]json.RawMessage{}
	}
	o.items[*ev.OutputIndex] = outputRef{typ: ev.Item.Type, id: ev.Item.ID}
	if item != nil {
		o.searches[*ev.OutputIndex] = item
	}
}

// searchItem is a finished web search as Codex sends it back: its type,
// ID, status, and action (codex-rs/protocol/src/models.rs).
func searchItem(ev streamEvent) json.RawMessage {
	item, err := json.Marshal(struct {
		Type   string           `json:"type"`
		ID     string           `json:"id,omitempty"`
		Status string           `json:"status,omitempty"`
		Action *webSearchAction `json:"action,omitempty"`
	}{webSearchCall, ev.Item.ID, ev.Item.Status, actionOrNil(ev.Item.Action)})
	if err != nil {
		return nil
	}

	return item
}

func actionOrNil(a webSearchAction) *webSearchAction {
	if a.Type == "" {
		return nil
	}

	return &a
}

// record keeps the successful attempt's searches, each anchored to the
// next output item the runner keeps, else the one before it.
func (s *stream) record() {
	s.mu.Lock()
	o := s.outputs
	s.mu.Unlock()
	if s.log == nil || len(o.searches) == 0 {
		return
	}
	indexes := slices.Sorted(maps.Keys(o.items))
	var records []searchRecord
	for _, i := range slices.Sorted(maps.Keys(o.searches)) {
		rec := searchRecord{At: time.Now(), Item: o.searches[i]}
		for _, j := range indexes {
			ref := o.items[j]
			if ref.typ == webSearchCall || ref.id == "" {
				continue
			}
			if j < i {
				rec.After = ref.id
			} else if rec.Before == "" {
				rec.Before = ref.id
			}
		}
		if rec.Before != "" || rec.After != "" {
			records = append(records, rec)
		}
	}
	if err := s.log.add(records); err != nil && s.log.logger != nil {
		s.log.logger.Warn("failed to record web searches", slog.String("error", err.Error()))
	}
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

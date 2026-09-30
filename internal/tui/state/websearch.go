package state

import "github.com/viktordanov/uah/internal/engine"

// WebSearchTool is the name a web search's transcript line shows: the
// provider's hosted tool (docs/design/web-search.md).
const WebSearchTool = "web_search"

// onWebSearch shows a hosted web search as a tool line: running while the
// provider searches, then what it searched or opened.
func (s *State) onWebSearch(e engine.WebSearch) {
	key := "web:" + e.ItemID
	if e.ItemID == "" {
		key = s.nextKey("web")
	}
	if !e.Done {
		s.put(Item{Kind: KindTool, Key: key, Name: WebSearchTool, Label: e.Text(), Tool: ToolRunning, Started: e.At})

		return
	}
	done := func(it *Item) {
		it.Label, it.Tool, it.Detail = e.Text(), ToolOK, "hosted"
		if !it.Started.IsZero() {
			it.Duration = e.At.Sub(it.Started)
		}
	}
	if !s.update(key, done) {
		it := Item{Kind: KindTool, Key: key, Name: WebSearchTool}
		done(&it)
		s.put(it)
	}
}

package embedded_test

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/engine/embedded"
	"github.com/viktordanov/uah/testing/fakellm"
)

const webSearchDef = `{"type":"web_search"}`

// offersWebSearch reports whether a request offered the hosted tool.
func offersWebSearch(req fakellm.Request) bool {
	for _, def := range req.ToolDefs {
		if strings.ReplaceAll(string(def), " ", "") == webSearchDef {
			return true
		}
	}

	return false
}

// TestEmbedded_OffersWebSearch: the hosted tool goes with each request when
// the engine offers it and the provider has it, and never otherwise.
func TestEmbedded_OffersWebSearch(t *testing.T) {
	t.Parallel()
	noSearch := embedded.DefaultProviders()
	for i := range noSearch {
		noSearch[i].WebSearch = false
	}
	tests := []struct {
		name      string
		on        bool
		providers []embedded.Provider
		want      bool
	}{
		{name: "on, on a provider with search", on: true, want: true},
		{name: "disabled", on: false},
		{name: "on, on a provider without search", on: true, providers: noSearch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, fakellm.Reply{Commands: []string{"true"}}, fakellm.Reply{Text: "ok"})
			eng := embedded.New(embedded.Config{StateDir: e.StateDir, Provider: "openai", Getenv: e.getenv, WebSearch: tt.on, Providers: tt.providers})
			s, ev := e.open(t, eng, "")
			_, err := s.Submit("look it up")
			require.NoError(t, err)
			ev.finished()

			reqs := e.llm.Requests()
			require.Len(t, reqs, 2)
			for _, req := range reqs {
				assert.Equal(t, tt.want, offersWebSearch(req))
				assert.Contains(t, req.ToolNames, "Bash", "the other tools stay")
			}
		})
	}
}

// TestEmbedded_ReportsWebSearches: a response's searches reach the session
// as they start and finish, before the answer, also when the session does
// not stream text; the next request has them back, each before the item
// it preceded.
func TestEmbedded_ReportsWebSearches(t *testing.T) {
	t.Parallel()
	e := newEnv(t, fakellm.Reply{Text: "Go 1.27", Searches: []fakellm.Search{
		{Query: "latest Go release"},
		{Action: "open_page", URL: "https://go.dev/dl/"},
	}}, fakellm.Reply{Text: "again"})
	eng := embedded.New(embedded.Config{StateDir: e.StateDir, Provider: "openai", Getenv: e.getenv, WebSearch: true})
	s, ev := e.open(t, eng, "")
	_, err := s.Submit("which Go is latest?")
	require.NoError(t, err)
	assert.Equal(t, "Go 1.27", ev.finished().Answer)

	var order []string
	for _, x := range ev.all {
		switch v := x.(type) {
		case engine.WebSearch:
			order = append(order, v.Text())
		case core.AssistantMessage:
			order = append(order, "answer")
		case engine.TextDelta:
			t.Error("the session does not stream text")
		}
	}
	assert.Equal(t, []string{
		"searching the web", "searched: latest Go release",
		"searching the web", "opened: https://go.dev/dl/",
		"answer",
	}, order)

	ev.idle()
	ask(t, s, ev, "and now?")
	reqs := e.llm.Requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, []string{"ws-1-0", "ws-1-1", "msg-1"}, inputIDs(reqs[1])[2:5], "the searches, before the answer they preceded")
	assert.JSONEq(t, `{"type":"web_search_call","id":"ws-1-0","status":"completed","action":{"type":"search","query":"latest Go release"}}`, string(reqs[1].Input[2]))
}

// inputIDs are the IDs of a request's input items ("" for none).
func inputIDs(req fakellm.Request) []string {
	out := make([]string, 0, len(req.Input))
	for _, item := range req.Input {
		var head struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(item, &head)
		out = append(out, head.ID)
	}

	return out
}

// searched reports whether a request's input has a web search item.
func searched(req fakellm.Request) bool {
	return slices.ContainsFunc(req.Input, func(item json.RawMessage) bool { return strings.Contains(string(item), `"web_search_call"`) })
}

var searchReply = fakellm.Reply{Text: "Go 1.27", Searches: []fakellm.Search{{Query: "latest Go release"}}}

// TestEmbedded_SearchesSurviveResume: the searches are in the session's
// sidecar, so a resumed session's requests have them too.
func TestEmbedded_SearchesSurviveResume(t *testing.T) {
	t.Parallel()
	e := newEnv(t, searchReply, fakellm.Reply{Text: "later"})
	s, ev := e.open(t, e.embedded(), "")
	ask(t, s, ev, "which Go?")
	id := s.ID()
	require.NoError(t, s.Close())
	assert.FileExists(t, filepath.Join(e.StateDir, "sessions", id+".websearch.jsonl"))

	s, ev = e.open(t, e.embedded(), id)
	ask(t, s, ev, "and now?")
	reqs := e.llm.Requests()
	require.Len(t, reqs, 2)
	assert.True(t, searched(reqs[1]), "the resumed session's request")
}

// TestEmbedded_SearchesLeaveWithTheirAnchor: a rewind, a compaction, or
// /clear removes the item a search came before, and the search with it;
// a compaction's summary request never gets one.
func TestEmbedded_SearchesLeaveWithTheirAnchor(t *testing.T) {
	t.Parallel()
	t.Run("rewind", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, searchReply, fakellm.Reply{Text: "two"}, fakellm.Reply{Text: "edited"})
		s, ev := e.open(t, e.embedded(), "")
		first := send(t, s, ev, "which Go?")
		send(t, s, ev, "second")
		require.NoError(t, s.Rewind(first))
		ev.until("Rewound", isA[engine.Rewound])
		send(t, s, ev, "which Go, really?")
		reqs := e.llm.Requests()
		require.Len(t, reqs, 3)
		assert.True(t, searched(reqs[1]))
		assert.False(t, searched(reqs[2]), "the answer it preceded is gone")
	})
	t.Run("compaction", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, searchReply, fakellm.Reply{Text: "SUMMARY"}, fakellm.Reply{Text: "after"})
		s, ev := e.open(t, e.compacting(0), "")
		ask(t, s, ev, "which Go?")
		require.NoError(t, s.Compact())
		ask(t, s, ev, "and now?")
		reqs := e.llm.Requests()
		require.Len(t, reqs, 3)
		assert.Contains(t, inputIDs(reqs[1]), "msg-1", "the summary request covers the answer")
		assert.False(t, searched(reqs[1]), "not a turn request")
		assert.False(t, searched(reqs[2]))
	})
	t.Run("clear", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, searchReply, fakellm.Reply{Text: "fresh"})
		s, ev := e.open(t, e.compacting(0), "")
		ask(t, s, ev, "which Go?")
		require.NoError(t, s.Clear())
		ask(t, s, ev, "hello")
		reqs := e.llm.Requests()
		require.Len(t, reqs, 2)
		assert.False(t, searched(reqs[1]))
	})
}

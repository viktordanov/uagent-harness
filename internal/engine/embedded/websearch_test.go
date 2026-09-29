package embedded_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/engine/embedded"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
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
// not stream text. The runner keeps no record of them, so the next
// request's input has none.
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

	_, err = s.Submit("and now?")
	require.NoError(t, err)
	ev.finished()
	reqs := e.llm.Requests()
	require.Len(t, reqs, 2)
	for _, item := range reqs[1].Input {
		assert.NotContains(t, string(item), "web_search_call", "the runner drops search items")
	}
}

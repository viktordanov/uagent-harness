package render_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// TestWebSearchLine: a hosted web search is a tool line, running while the
// provider searches, then what it searched or opened, in both views.
func TestWebSearchLine(t *testing.T) {
	live := func() state.State {
		return apply(base(), core.RunStarted{At: t0, RunID: "r1"}, core.TurnStarted{At: t0, Turn: 1})
	}
	started := apply(live(), engine.WebSearch{At: t0, ItemID: "ws1"}, state.Tick{Now: t0.Add(2 * time.Second)})
	assert.Contains(t, screen(started, ""), "WEB")
	assert.Contains(t, screen(started, ""), "searching the web")

	done := apply(started,
		engine.WebSearch{At: t0.Add(2 * time.Second), ItemID: "ws1", Done: true, Action: engine.WebSearchSearch, Query: "latest Go release"},
		engine.WebSearch{At: t0.Add(3 * time.Second), ItemID: "ws2", Done: true, Action: engine.WebSearchOpenPage, URL: "https://go.dev/dl/"},
	)
	got := screen(done, "")
	assert.Contains(t, got, "searched: latest Go release")
	assert.Contains(t, got, "opened: https://go.dev/dl/")
	assert.NotContains(t, got, "searching the web", "the finished search replaced its line")

	detailed := screen(apply(done, state.ToggleDetails{}), "")
	assert.Contains(t, detailed, "web_search")
	assert.Contains(t, detailed, "searched: latest Go release")
}

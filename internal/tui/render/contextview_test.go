package render_test

import (
	"testing"

	"github.com/viktordanov/uagent-harness/internal/contextusage"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

func TestScreen_Context(t *testing.T) {
	u := contextusage.Usage{
		Model: "gpt-5.5", Window: 272_000, Used: 41_000, Buffer: 27_200,
		Categories: []contextusage.Category{
			{Name: contextusage.SystemPrompt, Tokens: 3_000},
			{Name: contextusage.Instructions, Tokens: 2_000, Items: []contextusage.Item{{Name: "AGENTS.md", Tokens: 2_000}}},
			{Name: contextusage.Skills, Tokens: 1_000, Items: []contextusage.Item{{Name: "memoria", Tokens: 600}, {Name: "go", Tokens: 400}}},
			{Name: contextusage.Tools, Tokens: 8_000, Items: []contextusage.Item{{Name: "Bash", Tokens: 5_000}, {Name: "Read", Tokens: 3_000}}},
			{Name: contextusage.UserMessages, Tokens: 2_000},
			{Name: contextusage.Assistant, Tokens: 5_000},
			{Name: contextusage.ToolResults, Tokens: 20_000},
		},
	}
	s := apply(base(), state.ContextShown{Usage: u, OK: true})
	golden(t, "context", screen(s, ""))
}

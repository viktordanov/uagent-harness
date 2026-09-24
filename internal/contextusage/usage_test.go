package contextusage_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/unreallabsai/unreal-agent/harness/llm"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/contextusage"
)

func msg(role llm.Role, text string) llm.Item {
	return llm.Item{Type: llm.ItemMessage, Data: llm.Message{Role: role, Text: text}}
}

func TestAnalyze(t *testing.T) {
	system := "You run on Unreal Agent Harness.\n\nThe following skills provide specialized instructions for specific tasks.\n" +
		"<available_skills><skill><name>release</name><description>Cut a release</description><location>/s/release/SKILL.md</location></skill></available_skills>\n\n" +
		"You are an AI agent.\n# Project instructions\n\nFollow these instructions.\n\n## /repo/AGENTS.md\n\nUse tabs.\n## A heading inside the file\nMore.\n\n## /repo/svc/AGENTS.md\n\nService rules.\n"
	req := llm.Request{
		Model: llm.Model{ID: "gpt-test"},
		Input: []llm.Item{
			msg(llm.RoleSystem, system),
			msg(llm.RoleUser, "fix the parser please"),
			{Type: llm.ItemToolCall, Data: llm.ToolCall{CallID: "c1", Name: "Bash", Arguments: `{"command":"go test ./..."}`}},
			{Type: llm.ItemToolResult, Data: llm.ToolResult{CallID: "c1", Output: []llm.ToolResultOutput{{Kind: llm.ToolResultText, Value: "ok  pkg 0.1s"}}}},
			msg(llm.RoleAssistant, "Done."),
		},
		Tools: []llm.Tool{{Name: "Bash", Description: "Run a command"}, {Name: "mcp__docs__search", Description: "Search the docs"}},
	}

	u := contextusage.Analyze(req, 0, 272000, compaction.Settings{Percent: 90}, []string{"/repo/AGENTS.md", "/repo/svc/AGENTS.md"})

	assert.True(t, u.Estimated)
	assert.Equal(t, int64(27200), u.Buffer, "90% leaves a tenth of the window for compaction")
	names := map[string]contextusage.Category{}
	for _, c := range u.Categories {
		names[c.Name] = c
	}
	require.Contains(t, names, contextusage.Instructions)
	files := names[contextusage.Instructions].Items
	require.Len(t, files, 2, "a heading inside a file does not split it")
	assert.ElementsMatch(t, []string{"/repo/AGENTS.md", "/repo/svc/AGENTS.md"}, []string{files[0].Name, files[1].Name})
	assert.Equal(t, "release", names[contextusage.Skills].Items[0].Name)
	assert.Equal(t, "mcp__docs__search", names[contextusage.MCPTools].Items[0].Name)
	assert.Equal(t, "Bash", names[contextusage.Tools].Items[0].Name)
	for _, c := range []string{contextusage.SystemPrompt, contextusage.UserMessages, contextusage.Assistant, contextusage.ToolResults} {
		assert.Positive(t, names[c].Tokens, c)
	}

	scaled := contextusage.Analyze(req, 10000, 272000, compaction.Settings{}, nil)
	assert.False(t, scaled.Estimated)
	var sum int64
	for _, c := range scaled.Categories {
		sum += c.Tokens
	}
	assert.InDelta(t, 10000, sum, 10, "categories add up to the reported tokens")
	assert.Equal(t, int64(272000-10000), scaled.Free())
}

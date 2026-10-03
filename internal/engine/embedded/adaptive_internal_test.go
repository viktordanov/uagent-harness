package embedded

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah-core/harness/llm"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/compaction"
)

func TestAdaptiveRoute(t *testing.T) {
	msg := func(role llm.Role) llm.Item {
		return llm.Item{Type: llm.ItemMessage, Data: llm.Message{Role: role, Text: "x"}}
	}
	call := llm.Item{Type: llm.ItemToolCall, Data: llm.ToolCall{CallID: "c"}}
	result := llm.Item{Type: llm.ItemToolResult, Data: llm.ToolResult{CallID: "c"}}
	reasoning := llm.Item{Type: llm.ItemReasoning}
	system, user, assistant := msg(llm.RoleSystem), msg(llm.RoleUser), msg(llm.RoleAssistant)

	tests := []struct {
		name  string
		input []llm.Item
		want  effortChoice
	}{
		{"the first request", []llm.Item{system, user}, effortChoice{effort: "high", reason: "1-step: first request"}},
		{"results only", []llm.Item{system, user, reasoning, call, result}, effortChoice{effort: "medium", reason: "1-step: tool results only"}},
		{"several results after text", []llm.Item{system, user, assistant, call, result, result}, effortChoice{effort: "medium", reason: "1-step: tool results only"}},
		{"a user message with the results", []llm.Item{system, user, call, result, user}, effortChoice{effort: "high", reason: "1-step: user message"}},
		{"a steer before the results", []llm.Item{system, user, call, user, result}, effortChoice{effort: "high", reason: "1-step: user message"}},
		{"the model's text last", []llm.Item{system, user, assistant}, effortChoice{effort: "high", reason: "1-step: no tool results"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, adaptiveRouter{steps: 1}.route(tt.input, llm.ReasoningEffortHigh, false))
		})
	}
	two := adaptiveRouter{steps: 2}.route([]llm.Item{system, user, call, result}, llm.ReasoningEffortMax, true)
	assert.Equal(t, effortChoice{effort: llm.ReasoningEffortXHigh, reason: "2-steps: tool results only"}, two, "ultra, two steps down, on the plain client")
	kept := adaptiveRouter{steps: 2}.route([]llm.Item{system, user}, llm.ReasoningEffortMax, true)
	assert.Equal(t, effortChoice{effort: llm.ReasoningEffortMax, ultra: true, reason: "2-steps: first request"}, kept)
}

func TestLowerEffort(t *testing.T) {
	for in, want := range map[llm.ReasoningEffort][2]llm.ReasoningEffort{
		"low": {"low", "low"}, "medium": {"low", "low"}, "high": {"medium", "low"}, "xhigh": {"high", "medium"}, "max": {"xhigh", "high"},
	} {
		assert.Equal(t, want[0], lowerEffort(in, false, 1), in)
		assert.Equal(t, want[1], lowerEffort(in, false, 2), in)
	}
	assert.Equal(t, llm.ReasoningEffortMax, lowerEffort(llm.ReasoningEffortMax, true, 1), "ultra")
	assert.Equal(t, llm.ReasoningEffortXHigh, lowerEffort(llm.ReasoningEffortMax, true, 2), "ultra, two steps")
}

func TestEffortUpdate(t *testing.T) {
	msg := func(role llm.Role) llm.Item {
		return llm.Item{Type: llm.ItemMessage, Data: llm.Message{Role: role, Text: "x"}}
	}
	update := func(e llm.ReasoningEffort) llm.Item {
		return llm.Item{Type: llm.ItemConfigurationUpdate, Data: llm.ConfigurationUpdate{ReasoningEffort: e}}
	}
	call := llm.Item{Type: llm.ItemToolCall, Data: llm.ToolCall{CallID: "c"}}
	result := llm.Item{Type: llm.ItemToolResult, Data: llm.ToolResult{CallID: "c"}}
	system, user := msg(llm.RoleSystem), msg(llm.RoleUser)
	request := func(e llm.ReasoningEffort, input ...llm.Item) llm.Request {
		return llm.Request{Model: llm.Model{ID: "m", ReasoningEffort: e}, Input: input}
	}
	newSwitcher := func(steps int) *switcher {
		return &switcher{adaptive: adaptiveRouter{steps: steps}, base: "high", updates: func(model string) bool { return model == "m" }}
	}

	s := newSwitcher(2)
	assert.Equal(t, llm.ReasoningEffortHigh, s.effortUpdate(request("high", system, user)), "the first request's baseline")
	assert.Equal(t, effortChoice{effort: "high", reason: "2-steps: first request", updated: true}, *s.update)
	assert.Equal(t, llm.ReasoningEffortLow, s.effortUpdate(request("high", system, user, update("high"), call, result)), "a follow-up lowered")
	assert.Equal(t, effortChoice{effort: "low", reason: "2-steps: tool results only", updated: true}, *s.update)
	assert.Empty(t, s.effortUpdate(request("high", system, user, call, update("low"), result, call, result)), "still low")
	assert.Equal(t, llm.ReasoningEffortHigh, s.effortUpdate(request("high", system, user, call, update("low"), result, msg(llm.RoleAssistant), user)), "raised for a user message")

	off := newSwitcher(0)
	assert.Equal(t, llm.ReasoningEffortMedium, off.effortUpdate(request("medium", system, update("high"), user)), "a new effort without adaptive effort")
	assert.Empty(t, off.effortUpdate(request("high", system, update("high"), user)), "the effort the history set")

	other := newSwitcher(2)
	other.model = "other" // the live model
	assert.Empty(t, other.effortUpdate(request("high", system, user, call, result)), "an unsupported model")
	assert.Nil(t, other.update)
	other.model = ""
	other.variant.ultra = true
	assert.Empty(t, other.effortUpdate(request("max", system, user, call, result)), "ultra only the request carries")
	assert.Nil(t, other.update)
}

// recordingClient keeps the requests it gets.
type recordingClient struct{ reqs []llm.Request }

func (c *recordingClient) Respond(_ context.Context, req llm.Request, _ llm.RequestOptions) (llm.Response, error) {
	c.reqs = append(c.reqs, req)

	return llm.Response{}, nil
}

func (*recordingClient) Close() error { return nil }

// TestEffortUpdate_CompactionPinsTheRequest: a compaction drops the updates
// it covers, and the requests after it carry the effort they last set, as
// Codex sets a new baseline; the updates after it still apply.
func TestEffortUpdate_CompactionPinsTheRequest(t *testing.T) {
	msg := func(role llm.Role, text string) llm.Item {
		return llm.Item{Type: llm.ItemMessage, Data: llm.Message{Role: role, Text: text}}
	}
	update := func(e llm.ReasoningEffort) llm.Item {
		return llm.Item{Type: llm.ItemConfigurationUpdate, Data: llm.ConfigurationUpdate{ReasoningEffort: e}}
	}
	history := []llm.Item{msg(llm.RoleSystem, "sys"), msg(llm.RoleUser, "first"), update("high"), msg(llm.RoleAssistant, "a"),
		msg(llm.RoleUser, "second"), update("medium"), msg(llm.RoleAssistant, "b")}
	rec, err := compaction.NewRecordCovering(history, 6, "summary", compaction.TriggerManual, "m", time.Now())
	require.NoError(t, err)
	client := &recordingClient{}
	sw, err := newSwitcher("m", variant{}, 1, func(variant) (Client, error) { return client, nil })
	require.NoError(t, err)
	sw.base, sw.updates = "high", func(string) bool { return true }
	c := &compactor{ctx: t.Context(), next: sw, record: &rec, settled: true, emit: func(core.Event) {}}

	_, err = c.Respond(t.Context(), llm.Request{Model: llm.Model{ID: "m", ReasoningEffort: "high"}, Input: history}, llm.RequestOptions{})
	require.NoError(t, err)
	later := append(slices.Clone(history), msg(llm.RoleUser, "third"), update("low"))
	_, err = c.Respond(t.Context(), llm.Request{Model: llm.Model{ID: "m", ReasoningEffort: "high"}, Input: later}, llm.RequestOptions{})
	require.NoError(t, err)

	require.Len(t, client.reqs, 2)
	assert.Equal(t, llm.ReasoningEffortMedium, client.reqs[0].Model.ReasoningEffort, "pinned to the covered history's last effort")
	assert.NotContains(t, client.reqs[0].Input, update("medium"), "the covered updates dropped")
	assert.Equal(t, llm.ReasoningEffortMedium, client.reqs[1].Model.ReasoningEffort, "the pin holds")
	assert.Equal(t, llm.ReasoningEffortLow, client.reqs[1].Effort(), "a later update applies")
}

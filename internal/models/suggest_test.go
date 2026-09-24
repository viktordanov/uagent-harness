package models_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent-harness/internal/models"
)

func TestSuggest(t *testing.T) {
	ids := []string{"gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-5.6-luna", "gpt-5.5", "codex-auto-review"}
	cases := map[string][]string{
		"gpt-luna-6":  {"gpt-6-luna"},
		"GPT-6-SOL":   {"gpt-6-sol"},
		"gpt-6-lnua":  {"gpt-6-luna"},
		"gpt-6-soll":  {"gpt-6-sol"},
		"gpt-5.6luna": {"gpt-5.6-luna", "gpt-6-luna"},
		"claude-opus": {},
		"":            nil,
	}
	for in, want := range cases {
		got := models.Suggest(in, ids)
		if len(want) == 0 {
			assert.Empty(t, got, in)

			continue
		}
		assert.Equal(t, want, got, in)
	}
}

func TestCatalog_Lookup(t *testing.T) {
	c := models.Bundled(models.ProviderCodex)
	m, ok, near := c.Lookup("gpt-6-sol")
	assert.True(t, ok)
	assert.Empty(t, near)
	assert.Equal(t, "GPT-6-Sol", m.DisplayName)
	assert.True(t, m.SupportsPriority())
	assert.Equal(t, []string{"low", "medium", "high", "xhigh", "max", "ultra"}, m.ReasoningLevels)
	_, ok, near = c.Lookup("gpt-sol-6")
	assert.False(t, ok)
	assert.Equal(t, []string{"gpt-6-sol"}, near)
	assert.NotContains(t, c.IDs(), "gpt-5.4", "hidden models are not offered")
	assert.Equal(t, models.OriginNone, models.Bundled(models.ProviderOllama).Origin)
}

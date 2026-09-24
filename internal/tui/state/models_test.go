package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/models"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

func codexCatalog(origin models.Origin) models.Catalog {
	return models.Catalog{Provider: "openai-codex", Origin: origin, Models: []models.Model{
		{ID: "gpt-6-sol", DisplayName: "GPT-6-Sol", ContextWindow: 272000, ServiceTiers: []string{"priority"}},
		{ID: "gpt-6-luna", ContextWindow: 400000},
		{ID: "gpt-5.6-luna"},
		{ID: "gpt-secret", Hidden: true},
	}}
}

// TestMenu_Models loads the provider's list on the first /model and offers
// it as the argument's values.
func TestMenu_Models(t *testing.T) {
	s := opened()
	assert.Empty(t, s.Suggestions("/model "), "nothing until the list loads")
	s, effects := apply(s, state.DraftChanged{Draft: "/model"})
	require.Equal(t, []state.Effect{state.EffLoadModels{Provider: "openai-codex"}}, effects)
	_, effects = apply(s, state.DraftChanged{Draft: "/model "})
	assert.Empty(t, effects, "only once while loading")

	s, _ = apply(s, state.ModelsLoaded{Catalog: codexCatalog(models.OriginLive)})
	items := s.Suggestions("/model ")
	assert.Equal(t, []string{"gpt-6-sol", "gpt-6-luna", "gpt-5.6-luna"}, labels(items), "hidden models are not offered")
	assert.Equal(t, "GPT-6-Sol · 272k context · /fast", items[0].Help)
	assert.Equal(t, "/model gpt-6-luna", items[1].Draft)
	assert.Equal(t, []string{"gpt-6-luna", "gpt-5.6-luna"}, labels(s.Suggestions("/model luna")), "prefix matches first, then others")
	assert.Equal(t, []string{"gpt-6-luna"}, labels(s.Suggestions("/model gpt-6-l")))
	_, effects = apply(s, state.DraftChanged{Draft: "/model g"})
	assert.Empty(t, effects, "loaded")
}

// TestMenu_ModelsOnAccept loads the list when tab completes "/mo" to "/model ".
func TestMenu_ModelsOnAccept(t *testing.T) {
	_, effects := apply(opened(), state.MenuAccept{Draft: "/mo"})
	assert.Equal(t, []state.Effect{state.EffSetDraft{Text: "/model "}, state.EffLoadModels{Provider: "openai-codex"}}, effects)
}

func TestCommand_ModelChecksTheList(t *testing.T) {
	cases := map[string]struct {
		origin  models.Origin
		text    string
		notice  string
		applies bool
	}{
		"a listed model":             {origin: models.OriginLive, text: "/model gpt-6-luna", applies: true},
		"a hidden model still works": {origin: models.OriginCached, text: "/model gpt-secret", applies: true},
		"a typo, with a suggestion": {
			origin: models.OriginLive, text: "/model gpt-luna-6",
			notice: "gpt-luna-6 is not available on openai-codex; did you mean gpt-6-luna?",
		},
		"the bundled list passes anything": {origin: models.OriginBundled, text: "/model gpt-7", applies: true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, _ := apply(opened(), state.ModelsLoaded{Catalog: codexCatalog(c.origin)})
			s, effects := apply(s, state.Submit{Text: c.text})
			if c.applies {
				require.Len(t, effects, 1)
				assert.IsType(t, state.EffSetSettings{}, effects[0])

				return
			}
			assert.Empty(t, effects)
			last := s.Items[len(s.Items)-1]
			assert.Equal(t, state.KindNotice, last.Kind)
			assert.Equal(t, c.notice, last.Text)
		})
	}
	_, effects := apply(opened(), state.Submit{Text: "/model gpt-luna-6"})
	assert.Len(t, effects, 1, "no list: passes through as before")
}

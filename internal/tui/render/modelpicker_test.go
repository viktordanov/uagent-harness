package render_test

import (
	"testing"

	"github.com/viktordanov/uah/internal/models"
	"github.com/viktordanov/uah/internal/tui/state"
)

// TestScreen_ModelPicker draws /model's two steps: the provider's models
// with the current one marked, then the chosen model's efforts with its
// default selected.
func TestScreen_ModelPicker(t *testing.T) {
	list := models.Catalog{Provider: "openai-codex", Origin: models.OriginLive, Models: []models.Model{
		{
			ID: "gpt-6-sol", DisplayName: "GPT-6-Sol", ContextWindow: 272000, ServiceTiers: []string{"priority"},
			ReasoningLevels: []string{"low", "medium", "high", "xhigh", "max", "ultra"}, DefaultEffort: "low",
		},
		{
			ID: "gpt-6-luna", ContextWindow: 400000, ReasoningLevels: []string{"low", "medium", "high"}, DefaultEffort: "medium",
			ReasoningHelp: map[string]string{"low": "Fast responses with lighter reasoning", "high": "Maximizes reasoning depth"},
		},
	}}
	s := apply(base(), state.Submit{Text: "/model"})
	golden(t, "model-loading", screen(s, ""))
	s = apply(s, state.ModelsLoaded{Catalog: list})
	golden(t, "model-picker", screen(s, ""))
	s = apply(s, state.ModelPickMove{Delta: 1}, state.ModelPickEnter{})
	golden(t, "model-effort", screen(s, ""))
}

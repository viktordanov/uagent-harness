package state

import (
	"fmt"
	"strings"

	"github.com/viktordanov/uagent-harness/internal/models"
)

type (
	// EffLoadModels loads the provider's model list for /model.
	EffLoadModels struct{ Provider string }
	// ModelsLoaded carries the provider's model list.
	ModelsLoaded struct{ Catalog models.Catalog }
)

func (EffLoadModels) effect() {}

// loadModels starts loading the model list when the draft is /model and
// the current provider's list is neither loaded nor loading.
func (s *State) loadModels(draft string) Effect {
	if !strings.HasPrefix(draft, "/model") || s.Menu.modelsLoading {
		return nil
	}
	if s.Menu.Models != nil && s.Menu.Models.Provider == s.Settings.Provider {
		return nil
	}
	s.Menu.modelsLoading = true

	return EffLoadModels{Provider: s.Settings.Provider}
}

// catalog is the loaded list for the current provider, if any.
func (s State) catalog() (models.Catalog, bool) {
	if s.Menu.Models == nil || s.Menu.Models.Provider != s.Settings.Provider {
		return models.Catalog{}, false
	}

	return *s.Menu.Models, true
}

// modelSuggestions are the visible models whose ID starts with arg, then
// those that contain it.
func (s State) modelSuggestions(arg string) []Suggestion {
	c, ok := s.catalog()
	if !ok {
		return nil
	}
	var first, rest []Suggestion
	for _, m := range c.Visible() {
		if m.ID == arg {
			continue
		}
		sug := Suggestion{Label: m.ID, Help: modelHelp(m), Draft: "/model " + m.ID}
		switch {
		case strings.HasPrefix(m.ID, arg):
			first = append(first, sug)
		case strings.Contains(m.ID, arg):
			rest = append(rest, sug)
		}
	}

	return append(first, rest...)
}

// modelHelp is a model's menu help: its name, window, and fast mode.
func modelHelp(m models.Model) string {
	var parts []string
	if m.DisplayName != "" && m.DisplayName != m.ID {
		parts = append(parts, m.DisplayName)
	}
	if m.ContextWindow > 0 {
		parts = append(parts, fmt.Sprintf("%dk context", m.ContextWindow/1000))
	}
	if m.SupportsPriority() {
		parts = append(parts, "/fast")
	}

	return strings.Join(parts, " · ")
}

// checkModel rejects a model the provider's list lacks, with near misses.
// Without a list from the provider, any model passes.
func (s State) checkModel(id string) error {
	c, ok := s.catalog()
	if !ok {
		return nil
	}

	return c.Check(id)
}

package agents

import (
	"fmt"
	"slices"
	"strings"
)

// Model is one model spawn_agent may ask for.
type Model struct {
	Name string
	// Listed models show in Codex's model picker, and in the error for an
	// unknown model.
	Listed bool
}

// CodexModels is Codex's bundled model catalog at rust-v0.156.1
// (codex-rs/models-manager/models.json): the models a ChatGPT account can
// use with the openai-codex provider. Listed is its "list" visibility.
var CodexModels = []Model{
	{"gpt-6-astra", true},
	{"gpt-6-sol", true},
	{"gpt-6-luna", true},
	{"gpt-5.6-sol", true},
	{"gpt-5.6-terra", true},
	{"gpt-5.6-luna", true},
	{"gpt-daybreak-blue-latest", false},
	{"gpt-daybreak-red-latest", false},
	{"gpt-5.5", true},
	{"gpt-5.4", false},
	{"codex-auto-review", false},
}

// maxModelsShown is how many models Codex names for an unknown one
// (MAX_SPAWN_AGENT_MODEL_OVERRIDES).
const maxModelsShown = 5

// checkModel refuses a model the catalog does not have, with Codex's
// message, before the child starts. Codex checks the spawn call's model,
// else [agents] default_subagent_model; an empty catalog accepts any.
func (m *Manager) checkModel(requested string) error {
	requested = first(requested, m.cfg.Model)
	if requested == "" || len(m.cfg.Models) == 0 || slices.ContainsFunc(m.cfg.Models, func(x Model) bool { return x.Name == requested }) {
		return nil
	}
	var listed []string
	for _, x := range m.cfg.Models {
		if x.Listed && len(listed) < maxModelsShown {
			listed = append(listed, x.Name)
		}
	}

	return fmt.Errorf("Unknown model `%s` for spawn_agent. Available models: %s", requested, strings.Join(listed, ", ")) //nolint:staticcheck // Codex's message, word for word
}

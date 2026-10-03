package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/viktordanov/uah/internal/models"
	"github.com/viktordanov/uah/internal/session"
)

// DefaultCodexModel is the default model on openai-codex and openai: Codex
// rust-v0.159.1 ranks gpt-6.1-sol first in its catalog. OpenAI rolls a new
// model out by account, so a login whose list lacks it gets the provider's
// fallback model.
const DefaultCodexModel = "gpt-6.1-sol"

// FallbackCodexModel is openai-codex's model before gpt-6.1-sol.
const FallbackCodexModel = "gpt-6-sol"

// fallbackModels are the providers with a default model and the model each
// gets while its list lacks DefaultCodexModel: gpt-6-sol on openai-codex,
// and on openai the runner's default, gpt-6-astra, which led Codex's
// catalog at rust-v0.156.1.
var fallbackModels = map[string]string{CodexProvider: FallbackCodexModel, models.ProviderOpenAI: "gpt-6-astra"}

// DefaultModel is the provider's default model for its list:
// DefaultCodexModel when the list came from the provider and has it, else
// the provider's fallback. The bundled list cannot say what a login has.
func DefaultModel(provider string, c models.Catalog) string {
	if _, ok, _ := c.Lookup(DefaultCodexModel); ok && c.Authoritative() {
		return DefaultCodexModel
	}

	return fallbackModels[provider]
}

// SettleModel picks the default model from the provider's list when
// nothing named the model; a named model stays. Off openai-codex the
// session's model also reviews, unless review.model names another.
func (r *Resolved) SettleModel(c models.Catalog) {
	if !r.DefaultModel {
		return
	}
	old := r.Settings.Model
	r.Settings.Model = DefaultModel(r.Settings.Provider, c)
	if r.Settings.Provider != CodexProvider && r.Review.Model == old {
		r.Review.Model = r.Settings.Model
	}
}

// modelNotices are the warnings about the model's settings that its
// catalog entry does not take: the effort and model_verbosity.
func modelNotices(c models.Catalog, r Resolved) []string {
	var notices []string
	for _, notice := range []string{effortNotice(c, r.Settings), verbosityNotice(c, r.Settings.Model, r.Verbosity)} {
		if notice != "" {
			notices = append(notices, notice)
		}
	}

	return notices
}

// effortNotice warns when the model's catalog entry does not list the
// effort, such as ultra on gpt-6-luna; the provider decides, so the session
// still opens.
func effortNotice(c models.Catalog, s session.Settings) string {
	m, ok := c.Metadata(s.Model)
	if !ok || len(m.ReasoningLevels) == 0 || slices.Contains(m.ReasoningLevels, s.Effort) {
		return ""
	}

	return fmt.Sprintf("%s does not list effort %s (it lists %s); the provider may refuse it", s.Model, s.Effort, strings.Join(m.ReasoningLevels, ", "))
}

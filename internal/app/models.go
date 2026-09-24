package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/models"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// NewModels is the model catalog for the settings' provider and base URL,
// cached in the state directory.
func NewModels(stateDir string, s session.Settings, getenv func(string) string) *models.Manager {
	return models.New(models.Options{
		Dir: models.CacheDir(stateDir), Provider: s.Provider, BaseURL: s.BaseURL, Getenv: getenv,
	})
}

// useModels makes the catalog the process default (for context windows and
// subagent validation) and loads it from the cache, without the network.
func useModels(ctx context.Context, m *models.Manager) {
	models.SetDefault(m)
	m.Catalog(ctx, m.Provider(), models.Offline)
}

// checkModels asks the provider for its list, as a session would, and
// checks that the configured model is in it. An unknown model is a warning:
// the provider decides.
func checkModels(ctx context.Context, m *models.Manager, model string) Check {
	c := m.Catalog(ctx, m.Provider(), models.Online)
	if c.Err != nil {
		return warn("models", fmt.Sprintf("%s did not list its models (%v); using the %s list of %d", c.Provider, c.Err, c.Origin, len(c.Models)),
			"check the credentials and the network; unknown models are passed through")
	}
	if c.Origin == models.OriginNone || len(c.Models) == 0 {
		return warn("models", c.Provider+" lists no models", "for ollama, pull a model; otherwise check the account")
	}
	detail := fmt.Sprintf("%d models available to this login (%s list from %s)", len(c.Visible()), c.Origin, c.Provider)
	if model == "" {
		return ok("models", detail)
	}
	if err := c.Check(model); err != nil {
		return warn("models", detail+"; "+err.Error(), "pick one from `uah models`, or ignore this if the provider accepts it")
	}

	return ok("models", detail+"; "+model+" is in it")
}

// ModelLine is one line of `uah models`.
func ModelLine(m models.Model) string {
	parts := []string{m.ID}
	if m.ContextWindow > 0 {
		parts = append(parts, fmt.Sprintf("%dk context", m.ContextWindow/1000))
	}
	if m.DefaultEffort != "" {
		parts = append(parts, "effort "+m.DefaultEffort+" ("+strings.Join(m.ReasoningLevels, ", ")+")")
	}
	if m.SupportsPriority() {
		parts = append(parts, "fast")
	}
	if m.Hidden {
		parts = append(parts, "hidden")
	}

	return strings.Join(parts, " · ")
}

// ListModels resolves the provider as a session with these inputs would
// and returns its catalog: the cache while fresh, else the provider's list,
// or with refresh always the provider's.
func ListModels(ctx context.Context, in Inputs, refresh bool, getenv func(string) string) (models.Catalog, error) {
	stateDir, err := filepath.Abs(in.StateDir)
	if err != nil {
		return models.Catalog{}, fmt.Errorf("failed to resolve state dir: %w", err)
	}
	if in.Workspace, err = filepath.Abs(workspaceFor(in, session.Info{})); err != nil {
		return models.Catalog{}, fmt.Errorf("failed to resolve workspace: %w", err)
	}
	cfg, _, err := config.Load(in.ConfigPath, in.Workspace)
	if err != nil {
		return models.Catalog{}, usage(err)
	}
	r, err := Resolve(in, session.Info{}, cfg)
	if err != nil {
		return models.Catalog{}, err
	}
	strategy := models.OnlineIfUncached
	if refresh {
		strategy = models.Online
	}
	m := NewModels(stateDir, r.Settings, getenv)

	return m.Catalog(ctx, m.Provider(), strategy), nil
}

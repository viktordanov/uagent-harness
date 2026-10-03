package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/viktordanov/uah/internal/config"
	"github.com/viktordanov/uah/internal/models"
)

// Verbosities are model_verbosity's values, Codex's Verbosity: the
// Responses API's text.verbosity.
var Verbosities = []string{"low", "medium", "high"}

// pickVerbosity is --model-verbosity or its variable, else model_verbosity;
// "" leaves each model its catalog's default_verbosity.
func pickVerbosity(in Inputs, cfg config.Config) (string, error) {
	v := first(in.ModelVerbosity, cfg.ModelVerbosity)
	if v != "" && !slices.Contains(Verbosities, v) {
		return "", usage(fmt.Errorf("invalid model_verbosity %q (want %s)", v, strings.Join(Verbosities, ", ")))
	}

	return v, nil
}

// verbosityNotice is Codex's warning for a model_verbosity the model's
// catalog entry does not take: the request then carries no verbosity.
func verbosityNotice(c models.Catalog, model, verbosity string) string {
	if _, ignored := c.Verbosity(model, verbosity); !ignored {
		return ""
	}

	return "model_verbosity is set but ignored as the model does not support verbosity: " + model
}

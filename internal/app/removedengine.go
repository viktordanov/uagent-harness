package app

import (
	"strings"

	"github.com/viktordanov/uagent-harness/internal/config"
)

// removedEngine is the warning for a setting that still chooses an engine.
const removedEngine = "the process engine was removed in uah 1.2; uah always uses the embedded engine"

// EngineNotice is the warning a session shows once when --engine,
// UAH_ENGINE, or the configuration's engine key is set, whatever its
// value, and "" when none is. The setting is otherwise ignored.
func EngineNotice(in Inputs, cfg config.Config) string {
	if where := engineSettings(in, cfg); len(where) > 0 {
		return removedEngine + " (drop " + strings.Join(where, " and ") + ")"
	}

	return ""
}

// engineSettings name the settings that still choose an engine.
func engineSettings(in Inputs, cfg config.Config) []string {
	var where []string
	if in.Engine != "" {
		where = append(where, "--engine or "+EnvEngine)
	}
	if cfg.Engine != "" {
		where = append(where, "engine from the configuration")
	}

	return where
}

// checkEngine is `uah doctor`'s warning for a setting that still chooses
// an engine; ok is false when none does.
func checkEngine(in Inputs, cfg config.Config) (Check, bool) {
	where := engineSettings(in, cfg)
	if len(where) == 0 {
		return Check{}, false
	}

	return warn("engine", removedEngine, "drop "+strings.Join(where, " and ")), true
}

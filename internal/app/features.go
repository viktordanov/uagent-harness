package app

import (
	"os"
	"slices"
	"strings"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/engine/embedded"
	"github.com/viktordanov/uagent-harness/internal/engine/process"
	"github.com/viktordanov/uagent-harness/internal/hooks"
	"github.com/viktordanov/uagent-harness/internal/rules"
)

// usedFeatures are the features of the capability table (engine.Table)
// that the configuration asks for. The session shows a notice for each one
// its engine does not run, and `uah doctor` a warning. Features that are on
// by default, such as live input or subagents, count only when configured.
func usedFeatures(r Resolved, cfg config.Config, workspace string, loaded []rules.Rule) []engine.Feature {
	var used []engine.Feature
	add := func(on bool, f engine.Feature) {
		if on {
			used = append(used, f)
		}
	}
	add(anyMCPServer(cfg), engine.FeatureMCP)
	events := hookEvents(cfg)
	add(slices.Contains(events, hooks.PreToolUse), engine.FeaturePreToolUseHooks)
	add(slices.Contains(events, hooks.PermissionRequest), engine.FeaturePermissionHooks)
	add(slices.Contains(events, hooks.PreCompact), engine.FeaturePreCompactHooks)
	add(len(loaded) > 0, engine.FeatureRules)
	add(slices.ContainsFunc(loaded, func(rule rules.Rule) bool { return rule.Decision == rules.Prompt }), engine.FeaturePromptRules)
	add(r.Settings.Mode == approval.ModeAuto, engine.FeatureAutoMode)
	add(cfg.Agents.Enabled != nil && *cfg.Agents.Enabled, engine.FeatureSubagents)
	add(compactionConfigured(cfg), engine.FeatureCompaction)
	add(len(embedded.CodexSkills(workspace, os.Getenv)) > 0, engine.FeatureCodexSkills)

	return used
}

func anyMCPServer(cfg config.Config) bool {
	for _, s := range cfg.MCPServers {
		if s.IsEnabled() {
			return true
		}
	}

	return false
}

// hookEvents are the events with a configured hook, trusted or not.
func hookEvents(cfg config.Config) []hooks.Event {
	list, err := cfg.HookList()
	if err != nil {
		return nil
	}
	events := make([]hooks.Event, 0, len(list))
	for _, h := range list {
		events = append(events, h.Event)
	}

	return events
}

// compactionConfigured reports whether a compaction key is set.
func compactionConfigured(cfg config.Config) bool {
	return cfg.AutoCompactPercent != nil || cfg.ModelAutoCompactTokenLimit != 0 || cfg.CompactPrompt != "" ||
		cfg.ExperimentalCompactPromptFile != "" || cfg.CompactModel != ""
}

// engineCapabilities are the capabilities of the engine a session would
// get, without building it.
func engineCapabilities(r Resolved, gate string) engine.Capabilities {
	if r.Engine == EngineProcess {
		return process.Capabilities(gate != "")
	}

	return embedded.New(embedded.Config{Provider: r.Settings.Provider}).Capabilities()
}

// checkEngine reports what the engine does not run, and warns about each
// configured feature among those: the notices a session would show.
func checkEngine(r Resolved, cfg config.Config, workspace string, caps engine.Capabilities) Check {
	var loaded []rules.Rule
	if approver, err := newApprover(r, cfg, workspace); err == nil {
		loaded = approver.Rules()
	}
	unsupported := caps.Unsupported(usedFeatures(r, cfg, workspace, loaded))
	if len(unsupported) > 0 {
		notices := make([]string, 0, len(unsupported))
		for _, req := range unsupported {
			notices = append(notices, req.Notice(r.Engine))
		}

		return warn("engine", strings.Join(notices, "; "), "use --engine embedded, or drop what the "+r.Engine+" engine cannot run")
	}
	if lacks := caps.Summary(); lacks != "" {
		return ok("engine", r.Engine+", without "+lacks+"; nothing configured needs them")
	}

	return ok("engine", r.Engine+": runs every feature")
}

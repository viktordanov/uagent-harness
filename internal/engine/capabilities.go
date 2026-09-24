package engine

import (
	"slices"
	"strings"
)

// Capabilities says what an engine can do: what reaches a live run, and
// which features it runs at all. The session, the TUI, and `uah doctor`
// read them; none of them checks an engine's name.
type Capabilities struct {
	LiveInput   bool
	LiveEffort  bool
	LiveModel   bool
	ServiceTier bool
	// Compaction means the engine can compact the context (Run.Compact and
	// Options.Compact), and runs PreCompact hooks.
	Compaction bool
	// LiveMode means a permission mode change reaches a live run
	// (Run.SetMode); otherwise it applies from the next run.
	LiveMode bool
	// Rules means the command rules apply: allow runs a command outside
	// the sandbox, forbidden refuses it, and prompt asks (or, without
	// Approvals, refuses it with a reason).
	Rules bool
	// Approvals means a run can ask while it works: escalation out of the
	// sandbox, prompt rules, the auto-reviewer, Auto mode, and
	// PermissionRequest hooks.
	Approvals bool
	// ToolHooks means PreToolUse hooks run around each tool call.
	ToolHooks bool
	// MCP means the engine runs MCP servers (MCPLister).
	MCP bool
	// Subagents means the engine offers the agent tools.
	Subagents bool
	// ApplyPatch means Codex's apply_patch tool, where the model has it.
	ApplyPatch bool
	// CodexSkills means skills load from Codex's folders, not only the
	// runner's .harness/skills.
	CodexSkills bool
	// ContextUsage means /context can break down the last request
	// (ContextReporter).
	ContextUsage bool
	// Images means an image pasted into the prompt reaches the model with
	// the message (internal/images).
	Images bool
}

// Feature is something a session can use that not every engine runs.
type Feature string

// The features, in the order the capability table lists them.
const (
	FeatureLiveInput       Feature = "live input"
	FeatureLiveSettings    Feature = "live settings"
	FeatureFast            Feature = "fast mode"
	FeatureCompaction      Feature = "compaction"
	FeaturePreCompactHooks Feature = "PreCompact hooks"
	FeatureRules           Feature = "command rules"
	FeaturePromptRules     Feature = "prompt rules"
	FeatureApprovals       Feature = "approvals"
	FeatureAutoMode        Feature = "Auto mode"
	FeaturePermissionHooks Feature = "PermissionRequest hooks"
	FeaturePreToolUseHooks Feature = "PreToolUse hooks"
	FeatureMCP             Feature = "MCP servers"
	FeatureSubagents       Feature = "subagents"
	FeatureApplyPatch      Feature = "apply_patch"
	FeatureCodexSkills     Feature = "Codex skills"
	FeatureContextUsage    Feature = "/context"
	FeatureImages          Feature = "pasted images"
)

// Requirement is one row of the capability table: a feature, whether an
// engine's capabilities run it, and what happens on an engine that does
// not.
type Requirement struct {
	Feature Feature
	// Has reports whether the capabilities run the feature.
	Has func(Capabilities) bool
	// Without is what happens on an engine without it.
	Without string
}

// Notice is the one line a session shows when it is configured to use the
// feature and its engine does not run it.
func (r Requirement) Notice(engineName string) string {
	return string(r.Feature) + ": not supported by the " + engineName + " engine (" + r.Without + "); use the embedded engine"
}

// Table is the capability table: every feature some engine may not run.
var Table = []Requirement{
	{FeatureLiveInput, func(c Capabilities) bool { return c.LiveInput }, "a message sent while the agent works waits for the next run, and ctrl+enter restarts the run"},
	{FeatureLiveSettings, func(c Capabilities) bool { return c.LiveEffort && c.LiveModel && c.LiveMode }, "/model, /effort, and the permission mode apply from the next run"},
	{FeatureFast, func(c Capabilities) bool { return c.ServiceTier }, "/fast is not available"},
	{FeatureCompaction, func(c Capabilities) bool { return c.Compaction }, "/compact and /clear are not available and the context is never compacted"},
	{FeaturePreCompactHooks, func(c Capabilities) bool { return c.Compaction }, "they never run, since nothing is compacted"},
	{FeatureRules, func(c Capabilities) bool { return c.Rules }, "no command rule applies"},
	{FeaturePromptRules, func(c Capabilities) bool { return c.Approvals }, "a command they match is not run, since no one can approve it during a run"},
	{FeatureApprovals, func(c Capabilities) bool { return c.Approvals }, "commands cannot ask to leave the sandbox and nothing is reviewed"},
	{FeatureAutoMode, func(c Capabilities) bool { return c.Approvals }, "it is the workspace sandbox without the auto-reviewer"},
	{FeaturePermissionHooks, func(c Capabilities) bool { return c.Approvals }, "they never run, since a run cannot ask"},
	{FeaturePreToolUseHooks, func(c Capabilities) bool { return c.ToolHooks }, "they do not run"},
	{FeatureMCP, func(c Capabilities) bool { return c.MCP }, "they do not start"},
	{FeatureSubagents, func(c Capabilities) bool { return c.Subagents }, "the agent tools are not offered"},
	{FeatureApplyPatch, func(c Capabilities) bool { return c.ApplyPatch }, "the model edits files with commands"},
	{FeatureCodexSkills, func(c Capabilities) bool { return c.CodexSkills }, "only the runner's .harness/skills load"},
	{FeatureContextUsage, func(c Capabilities) bool { return c.ContextUsage }, "it is not available"},
	{FeatureImages, func(c Capabilities) bool { return c.Images }, "an image cannot be attached to a message; the model can still open an image file with its ViewImage tool"},
}

// Lacks is every row of the table the capabilities do not run, in table
// order.
func (c Capabilities) Lacks() []Requirement {
	var out []Requirement
	for _, r := range Table {
		if !r.Has(c) {
			out = append(out, r)
		}
	}

	return out
}

// Unsupported is the rows for the features used that the capabilities do
// not run, in table order.
func (c Capabilities) Unsupported(used []Feature) []Requirement {
	var out []Requirement
	for _, r := range c.Lacks() {
		if slices.Contains(used, r.Feature) {
			out = append(out, r)
		}
	}

	return out
}

// Summary names the features the capabilities lack in one line ("" when
// they run every feature), for /status and `uah doctor`.
func (c Capabilities) Summary() string {
	lacks := c.Lacks()
	names := make([]string, 0, len(lacks))
	for _, r := range lacks {
		names = append(names, string(r.Feature))
	}

	return strings.Join(names, ", ")
}

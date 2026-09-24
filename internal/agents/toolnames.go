package agents

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	rtool "github.com/unreallabsai/unreal-agent/harness/tool"

	"github.com/viktordanov/uagent-harness/internal/mcp"
	"github.com/viktordanov/uagent-harness/internal/patch"
	"github.com/viktordanov/uagent-harness/internal/rules"
)

// toolAliases maps the tool names an agent definition may use, uah's and
// Claude Code's, to uah's tools. Claude Code edits files with Edit, Write,
// MultiEdit, and NotebookEdit; uah's model does it with apply_patch.
var toolAliases = map[string]string{
	rtool.BashName: rtool.BashName, rtool.ViewImageName: rtool.ViewImageName, rtool.SkillUseName: rtool.SkillUseName, "Skill": rtool.SkillUseName,
	patch.ToolName: patch.ToolName, "Edit": patch.ToolName, "Write": patch.ToolName,
	"MultiEdit": patch.ToolName, "NotebookEdit": patch.ToolName,
}

// readTools are Claude Code's tools for reading and searching files; uah's
// agents do that with Bash.
var readTools = []string{"Read", "Grep", "Glob", "LS"}

// spawnTools are the tools that start subagents, uah's and Claude Code's;
// a subagent is never offered them.
var spawnTools = []string{"spawn_agent", "send_input", "wait_agent", "close_agent", "resume_agent", "Agent", "Task"}

// mapTools turns a definition's tools into uah's tool names. The result is
// non-nil, so a list whose names all map to nothing still offers no tools
// rather than every tool.
func mapTools(names []string) (tools, warnings []string) {
	tools = []string{}
	add := func(n string) {
		if !slices.Contains(tools, n) {
			tools = append(tools, n)
		}
	}
	for _, raw := range names {
		name, spec, _ := strings.Cut(strings.TrimSpace(raw), "(")
		switch {
		case name == "":
			continue
		case spec != "":
			warnings = append(warnings, fmt.Sprintf("tools: %s: the part in parentheses is ignored; list commands under approve", raw))
		}
		switch uah, ok := toolAliases[name]; {
		case ok:
			add(uah)
		case strings.HasPrefix(name, mcp.Prefix):
			add(name)
		case slices.Contains(readTools, name):
			warnings = append(warnings, fmt.Sprintf("tools: %s: uah's agents read and search files with Bash; add Bash to allow it", name))
		case slices.Contains(spawnTools, name):
			warnings = append(warnings, fmt.Sprintf("tools: %s: subagents never start subagents", name))
		default:
			warnings = append(warnings, fmt.Sprintf("tools: %s: uah has no such tool", name))
		}
	}

	return tools, warnings
}

// mapApprove turns a definition's approve entries into what the engine
// pre-approves: command prefixes and MCP tool names or server patterns.
// Claude Code's permission rule form, Bash(git diff *) or Bash(git
// diff:*), is read as the prefix git diff; Edit and Write as apply_patch.
func mapApprove(entries []string) (approve, warnings []string) {
	for _, raw := range entries {
		entry, err := approveEntry(strings.TrimSpace(raw))
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("approve: %q: %v", raw, err))

			continue
		}
		if entry != "" && !slices.Contains(approve, entry) {
			approve = append(approve, entry)
		}
	}

	return approve, warnings
}

// approveEntry reads one approve entry.
func approveEntry(entry string) (string, error) {
	if strings.HasPrefix(entry, mcp.Prefix) || entry == "" {
		return entry, nil
	}
	if uah, ok := toolAliases[entry]; ok {
		if uah != patch.ToolName {
			return "", errors.New("a whole tool cannot be approved; approve command prefixes or MCP tools")
		}

		return patch.ToolName, nil
	}
	if inner, ok := strings.CutPrefix(entry, "Bash("); ok {
		inner, ok = strings.CutSuffix(inner, ")")
		if !ok {
			return "", errors.New("want Bash(<command prefix>)")
		}
		entry = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(inner), "*"), ":"))
	}
	if entry == "" {
		return "", errors.New("an empty command would approve every command")
	}
	if _, err := rules.FromPrefixes([]string{entry}, rules.Allow, "approve"); err != nil {
		return "", errors.New("not a command prefix (plain words, no operators or globs)")
	}

	return entry, nil
}

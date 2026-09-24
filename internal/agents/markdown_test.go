package agents_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/agents"
)

// TestLoadRoles_Markdown reads Claude Code's agent files: the front
// matter's keys map onto a role, Claude Code's tool names onto uah's, and
// the body is the instructions.
func TestLoadRoles_Markdown(t *testing.T) {
	dir := t.TempDir()
	writeRole(t, dir, "reviewer.md", `---
name: reviewer
description: Reviews diffs for correctness bugs.
tools: Bash, Edit, Write, Read, mcp__github, WebFetch
model: gpt-6-luna
effort: low
fast: true
color: blue
approve:
  - git diff
  - Bash(git log:*)
  - Bash(rm -rf *)
  - Edit
  - mcp__github__get_issue
  - Bash(*)
  - git status && rm x
---

Review the diff you are given.
List only real bugs.
`)
	writeRole(t, filepath.Join(dir, "sub"), "explorer.md", "---\nname: explorer\ndescription: Explores.\ntools: [Read, Grep]\nmodel: inherit\nmodel_reasoning_effort: medium\n---\nExplore.\n")
	writeRole(t, dir, "claude.md", "---\nname: claude\ndescription: Uses a Claude alias.\nmodel: sonnet\n---\nWork.\n")
	writeRole(t, dir, "plain.md", "No front matter.\n")

	roles, warnings := agents.LoadRoles(dir)

	require.Len(t, roles, 3)
	claude, explorer, reviewer := roles[0], roles[1], roles[2]
	assert.Empty(t, claude.Model, "a Claude alias names no model here: the parent's")
	assert.Nil(t, claude.Tools, "no tools key: every tool")

	assert.Empty(t, explorer.Model, "inherit is the parent's model")
	assert.Equal(t, "medium", explorer.Effort)
	assert.Equal(t, []string{}, explorer.Tools, "Read and Grep map to nothing, which offers no tools rather than all")

	assert.Equal(t, "Reviews diffs for correctness bugs.", reviewer.Description)
	assert.Equal(t, "Review the diff you are given.\nList only real bugs.", reviewer.DeveloperInstructions)
	assert.Equal(t, [3]string{"gpt-6-luna", "low", agents.TierPriority}, [3]string{reviewer.Model, reviewer.Effort, reviewer.ServiceTier})
	assert.Equal(t, []string{"Bash", "apply_patch", "mcp__github"}, reviewer.Tools)
	assert.Equal(t, []string{"git diff", "git log", "rm -rf", "apply_patch", "mcp__github__get_issue"}, reviewer.Approve)
	assert.Equal(t, filepath.Join(dir, "reviewer.md"), reviewer.Path)

	all := joinLines(warnings)
	assert.Contains(t, all, `model = "sonnet" (a Claude model alias`)
	assert.Contains(t, all, "tools: Read: uah's agents read and search files with Bash")
	assert.Contains(t, all, "tools: WebFetch: uah has no such tool")
	assert.Contains(t, all, "ignoring keys uah does not support: color")
	assert.Contains(t, all, `approve: "Bash(*)": an empty command would approve every command`)
	assert.Contains(t, all, `approve: "git status && rm x": not a command prefix`)
	assert.Contains(t, all, "plain.md: an agent file starts with front matter")
}

// TestLoadRoles_MarkdownBesideTOML resolves a name defined twice: within
// a directory the Markdown file wins, with a warning; a later directory
// replaces an earlier one, whatever the format.
func TestLoadRoles_MarkdownBesideTOML(t *testing.T) {
	user, project := t.TempDir(), t.TempDir()
	writeRole(t, user, "worker.toml", "name = \"worker\"\ndescription = \"TOML worker.\"\ndeveloper_instructions = \"Work.\"\n")
	writeRole(t, user, "worker.md", "---\nname: worker\ndescription: Markdown worker.\n---\nWork.\n")
	writeRole(t, user, "helper.md", "---\nname: helper\ndescription: User helper.\n---\nHelp.\n")
	writeRole(t, project, "helper.toml", "name = \"helper\"\ndescription = \"Project helper.\"\ndeveloper_instructions = \"Help here.\"\n")

	roles, warnings := agents.LoadRoles(user, project)

	require.Len(t, roles, 2)
	assert.Equal(t, "Project helper.", roles[0].Description, "the later directory wins")
	assert.Equal(t, "Markdown worker.", roles[1].Description, "the Markdown file wins in its directory")
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], `agent "worker" is defined twice in `+user+"; using "+filepath.Join(user, "worker.md"))
}

// TestLoadRoles_MarkdownChecks applies Codex's rules to a Markdown file.
func TestLoadRoles_MarkdownChecks(t *testing.T) {
	dir := t.TempDir()
	writeRole(t, dir, "nobody.md", "---\nname: nobody\ndescription: Has no instructions.\n---\n\n")
	writeRole(t, dir, "open.md", "---\nname: open\n")
	writeRole(t, dir, "bad.md", "---\nname: [bad\n---\nx\n")

	roles, warnings := agents.LoadRoles(dir)

	assert.Empty(t, roles)
	all := joinLines(warnings)
	assert.Contains(t, all, "must define developer_instructions")
	assert.Contains(t, all, "no closing --- line")
	assert.Contains(t, all, "failed to parse the front matter")
}

func joinLines(lines []string) string { return strings.Join(lines, "\n") }

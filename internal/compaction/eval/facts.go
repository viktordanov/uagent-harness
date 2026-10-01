package eval

import (
	"strings"

	"github.com/viktordanov/unreal-agent/harness/llm"

	"github.com/viktordanov/uah/internal/compaction"
)

// Kind is a kind of fact the recall counts.
type Kind string

const (
	KindChanged Kind = "changed files"
	KindFailing Kind = "failing commands"
	// KindTail is the last line of a failing command's stderr.
	KindTail   Kind = "stderr tails"
	KindRead   Kind = "paths read"
	KindSkills Kind = "skills"
	KindAgents Kind = "open subagents"
	KindFocus  Kind = "focus"
)

// toolBash is the runner's shell tool.
const toolBash = "Bash"

// Kinds are the fact kinds, in the order tables list them.
var Kinds = []Kind{KindChanged, KindFailing, KindTail, KindRead, KindSkills, KindAgents, KindFocus}

// recall counts the facts found in text. A failing command is found when
// its first three words are; its tail when the tail's last line is.
func recall(f compaction.Facts, focus, text string) map[Kind]Count {
	out := map[Kind]Count{}
	count := func(k Kind, found bool) {
		c := out[k]
		c.Total++
		if found {
			c.Found++
		}
		out[k] = c
	}
	for _, c := range f.Changed {
		count(KindChanged, present(c.Path, text))
	}
	flat := words(text)
	for _, c := range f.Failing {
		count(KindFailing, strings.Contains(text, firstWords(c.Command, 3)))
		if tail := words(lastLine(c.Tail)); tail != "" {
			count(KindTail, strings.Contains(flat, tail))
		}
	}
	for _, p := range f.Read {
		count(KindRead, present(p, text))
	}
	for _, s := range f.Skills {
		count(KindSkills, strings.Contains(text, s))
	}
	for _, a := range f.Agents {
		id, _, _ := strings.Cut(a, " ")
		count(KindAgents, strings.Contains(text, id))
	}
	if focus = strings.TrimSpace(focus); focus != "" {
		count(KindFocus, strings.Contains(text, focus))
	}

	return out
}

// words is s with its whitespace collapsed, so text that a summary or the
// ledger re-flowed still matches.
func words(s string) string { return strings.Join(strings.Fields(s), " ") }

func firstWords(s string, n int) string {
	f := strings.Fields(s)

	return strings.Join(f[:min(n, len(f))], " ")
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")

	return strings.TrimSpace(lines[len(lines)-1])
}

// userKept counts the covered user messages' tokens that the request after
// still has word for word.
func userKept(covered, after []llm.Item) Count {
	have := map[string]bool{}
	for _, item := range after {
		if m, ok := item.Data.(llm.Message); ok && m.Role == llm.RoleUser {
			have[m.Text] = true
		}
	}
	var c Count
	for _, item := range covered {
		m, ok := item.Data.(llm.Message)
		if !ok || !compaction.IsUserMessage(item) || compaction.IsHeartbeat(item) {
			continue
		}
		tokens := compaction.ApproxTokens(m.Text)
		c.Total += tokens
		if have[m.Text] {
			c.Found += tokens
		}
	}

	return c
}

// skillsKept counts the covered SkillUse bodies still in the request after.
func skillsKept(covered, after []llm.Item) Count {
	have := map[string]bool{}
	for _, body := range skillBodies(after) {
		have[body] = true
	}
	var c Count
	for _, body := range skillBodies(covered) {
		c.Total++
		if have[body] {
			c.Found++
		}
	}

	return c
}

func skillBodies(items []llm.Item) []string {
	skills := map[string]bool{}
	var out []string
	for _, item := range items {
		switch d := item.Data.(type) {
		case llm.ToolCall:
			skills[d.CallID] = d.Name == "SkillUse"
		case llm.ToolResult:
			if skills[d.CallID] {
				out = append(out, compaction.ResultText(d))
			}
		}
	}

	return out
}

// refetch counts the later calls (up to MaxLater) that name a path read
// before the cut, or repeat a command, whose output the request after no
// longer has: the calls a strategy makes the model spend on reading again.
func refetch(covered, after []llm.Item, later []llm.ToolCall) Count {
	seen := map[string]bool{} // paths and commands before the cut
	for _, item := range covered {
		if c, ok := item.Data.(llm.ToolCall); ok && c.Name == toolBash {
			cmd := command(c)
			seen[cmd] = true
			for _, p := range compaction.PathsIn(cmd) {
				seen[p] = true
			}
		}
	}
	kept := retained(after)
	var n Count
	for _, c := range later {
		if c.Name != toolBash || n.Total == MaxLater {
			continue
		}
		n.Total++
		cmd := command(c)
		keys := append([]string{cmd}, compaction.PathsIn(cmd)...)
		for _, k := range keys {
			if seen[k] && !kept[k] {
				n.Found++

				break
			}
		}
	}

	return n
}

// retained are the commands and paths whose output the request still has
// in full.
func retained(items []llm.Item) map[string]bool {
	calls := map[string]string{}
	out := map[string]bool{}
	for _, item := range items {
		switch d := item.Data.(type) {
		case llm.ToolCall:
			if d.Name == toolBash {
				calls[d.CallID] = command(d)
			}
		case llm.ToolResult:
			cmd, ok := calls[d.CallID]
			if !ok || strings.HasPrefix(compaction.ResultText(d), "[uah elided") {
				continue
			}
			out[cmd] = true
			for _, p := range compaction.PathsIn(cmd) {
				out[p] = true
			}
		}
	}

	return out
}

func command(c llm.ToolCall) string {
	return strings.TrimSpace(compaction.Argument(c.Arguments, "command"))
}

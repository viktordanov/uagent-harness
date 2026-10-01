package compaction

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/viktordanov/unreal-agent/harness/llm"
)

// LedgerMaxTokens caps the state ledger. Each list keeps its newest entries
// and says how many it left out.
const LedgerMaxTokens = 850

// The ledger's share of LedgerMaxTokens, in bytes, per section.
const (
	ledgerChangedBytes = 1000
	ledgerFailingBytes = 1100
	ledgerReadBytes    = 700
	ledgerSkillsBytes  = 250
	ledgerAgentsBytes  = 250
	ledgerFocusBytes   = 100
	// ledgerTailBytes bounds one failing command's stderr tail.
	ledgerTailBytes = 240
)

// Ledger is the state ledger uah appends to a summary: the facts of the
// covered items (ExtractFacts) in a fixed, compact format, so what the
// next turns rely on survives however the model wrote the summary. It is
// empty when the items established none of it.
func Ledger(covered []llm.Item, focus string) string {
	f := ExtractFacts(covered)
	var sections []string
	add := func(title string, entries []string, budget int) {
		if len(entries) > 0 {
			sections = append(sections, title+": "+fit(entries, budget))
		}
	}
	add("Changed files (apply_patch)", byDirectory(changedEntries(f.Changed)), ledgerChangedBytes)
	add("Failing commands (their last run failed)", failingEntries(f.Failing), ledgerFailingBytes)
	add("Files read", byDirectory(f.Read), ledgerReadBytes)
	add("Skills loaded (SkillUse again for the body)", f.Skills, ledgerSkillsBytes)
	add("Open subagents", f.Agents, ledgerAgentsBytes)
	if focus = strings.TrimSpace(focus); focus != "" {
		sections = append(sections, "Compaction focus: "+clip(oneLine(focus), ledgerFocusBytes))
	}
	if len(sections) == 0 {
		return ""
	}

	return "<uah_state_ledger>\nFacts uah read from the tool calls this summary replaces:\n" + strings.Join(sections, "\n") + "\n</uah_state_ledger>"
}

func changedEntries(changes []FileChange) []string {
	out := make([]string, 0, len(changes))
	for _, c := range changes {
		switch {
		case c.Deleted:
			out = append(out, c.Path+" (deleted)")
		case c.Created:
			out = append(out, fmt.Sprintf("%s (new, +%d)", c.Path, c.Added))
		default:
			out = append(out, fmt.Sprintf("%s (+%d −%d)", c.Path, c.Added, c.Removed))
		}
	}

	return out
}

func failingEntries(failed []FailedCommand) []string {
	out := make([]string, 0, len(failed))
	for _, c := range failed {
		entry := fmt.Sprintf("%#q exit %d", clip(oneLine(c.Command), 160), c.Exit)
		if tail := clipStart(oneLine(c.Tail), ledgerTailBytes); tail != "" {
			entry += ": " + tail
		}
		out = append(out, entry)
	}

	return out
}

// byDirectory groups entries that are paths (with an optional note after
// a space) by their directory, "dir/{a.go, b.go (+2 −1)}", ordered by each
// directory's newest entry, so a long list of files takes less room.
func byDirectory(entries []string) []string {
	var dirs []string
	files := map[string][]string{}
	for _, e := range entries {
		p, note, _ := strings.Cut(e, " ")
		dir, file := path.Split(p)
		if note != "" {
			file += " " + note
		}
		dirs = append(slices.DeleteFunc(dirs, func(d string) bool { return d == dir }), dir)
		files[dir] = append(files[dir], file)
	}
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if f := files[dir]; len(f) == 1 {
			out = append(out, dir+f[0])
		} else {
			out = append(out, dir+"{"+strings.Join(f, ", ")+"}")
		}
	}

	return out
}

// fit joins the newest entries that fit in budget bytes, oldest first, and
// counts the ones left out.
func fit(entries []string, budget int) string {
	entries = slices.Clone(entries)
	for i := range entries {
		entries[i] = clip(entries[i], budget)
	}
	kept, used := 0, 0
	for _, e := range slices.Backward(entries) {
		if used+len(e)+2 > budget && kept > 0 {
			break
		}
		used += len(e) + 2
		kept++
	}
	out := strings.Join(entries[len(entries)-kept:], "; ")
	if left := len(entries) - kept; left > 0 {
		out = fmt.Sprintf("(%d earlier not listed) %s", left, out)
	}

	return out
}

// oneLine joins text's lines with " ⏎ " and trims it.
func oneLine(text string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(strings.TrimSpace(text), "\n", " ⏎ ")), " ")
}

// clipStart shortens text to at most n bytes from its end, with "…".
func clipStart(text string, n int) string {
	if len(text) <= n {
		return text
	}
	cut := len(text) - n + len("…")
	for cut < len(text) && !utf8.RuneStart(text[cut]) {
		cut++
	}

	return "…" + text[cut:]
}

// clip shortens text to at most n bytes on a rune boundary, with "…".
func clip(text string, n int) string {
	if len(text) <= n {
		return text
	}
	cut := n - len("…")
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}

	return text[:cut] + "…"
}

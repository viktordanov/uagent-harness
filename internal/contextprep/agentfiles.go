package contextprep

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// AgentFiles' limits: the whole block, the included files, the inlined
// skills, and one skill.
const (
	agentFilesMax = 10 << 10
	includesMax   = 3 << 10
	skillsMax     = 6 << 10
	skillMax      = 4 << 10
)

// Skill is a skill the session offers, as its SKILL.md names it.
type Skill struct {
	Name        string
	Description string
	// Path is the skill's SKILL.md.
	Path string
}

// AgentFiles is the "agent files" block: the instruction files the system
// prompt holds, said to be all of them so the model does not search for
// more; the files they include with an @ line, which the system prompt does
// not expand; and the bodies of the skills whose description says they
// apply to every message, so the model need not load them.
type AgentFiles struct {
	// Skills are the session's skills, most specific first.
	Skills []Skill
}

// Name is the block's name.
func (AgentFiles) Name() string { return "agent files" }

// MaxBytes is the block's cap: it carries other files' text.
func (AgentFiles) MaxBytes() int { return agentFilesMax }

// Prepare lists the instruction files and inlines the includes and the
// skills that always apply.
func (a AgentFiles) Prepare(_ context.Context, f Facts) string {
	files, includes := instructionFiles(f.SystemPrompt)
	var parts []string
	if len(files) == 0 {
		parts = append(parts, "No instruction files (AGENTS.md) were loaded for this session, so there are none to search for.")
	} else {
		parts = append(parts, "Instruction files in the system prompt, in order:\n- "+strings.Join(files, "\n- ")+
			"\nThese are all of the session's instruction files: there is no need to search for more AGENTS.md or CLAUDE.md files.")
	}
	if inc := inlineIncludes(includes); inc != "" {
		parts = append(parts, inc)
	}
	if sk := inlineSkills(a.Skills); sk != "" {
		parts = append(parts, sk)
	}

	return strings.Join(parts, "\n\n")
}

// include is an @ line of an instruction file: the line's path and the
// file it names.
type include struct{ from, line, path string }

// instructionFiles are the instruction files the system prompt holds, each
// under a "## <path>" header (instructions.Assemble), and the @ lines in
// them, a relative one resolved against its file's directory.
func instructionFiles(systemPrompt string) ([]string, []include) {
	var files []string
	var includes []include
	current := ""
	sc := bufio.NewScanner(strings.NewReader(systemPrompt))
	sc.Buffer(nil, len(systemPrompt)+1)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if p, ok := strings.CutPrefix(line, "## "); ok && filepath.IsAbs(p) {
			if _, err := os.Stat(p); err == nil {
				current = p
				files = append(files, p)

				continue
			}
		}
		p, ok := strings.CutPrefix(line, "@")
		if !ok || p == "" || strings.ContainsAny(p, " \t") || current == "" {
			continue
		}
		includes = append(includes, include{from: current, line: p, path: resolveInclude(filepath.Dir(current), p)})
	}

	return files, includes
}

// inlineIncludes is the included files' text, each under its @ line, up to
// includesMax; a file that does not fit is named so the model can read it.
func inlineIncludes(includes []include) string {
	var b strings.Builder
	var seen, left []string
	for _, inc := range includes {
		if slices.Contains(seen, inc.path) {
			continue
		}
		seen = append(seen, inc.path)
		data, err := os.ReadFile(inc.path)
		if err != nil {
			continue
		}
		body := strings.TrimSpace(string(data))
		if b.Len()+len(body) > includesMax {
			left = append(left, inc.path)

			continue
		}
		fmt.Fprintf(&b, "%s includes @%s (%s), so you need not read it:\n%s\n\n", inc.from, inc.line, inc.path, body)
	}
	if len(left) > 0 {
		fmt.Fprintf(&b, "Also included, too long to show here; read them when you need them: %s", strings.Join(left, ", "))
	}

	return strings.TrimSpace(b.String())
}

// resolveInclude makes an include's path absolute: ~/ from the home
// directory, a relative one from dir.
func resolveInclude(dir, p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}

	return filepath.Join(dir, p)
}

// alwaysPattern matches a skill description that says the skill applies to
// every message: "whenever responding to ANY user message", "on every
// response", "always apply this skill".
var alwaysPattern = regexp.MustCompile(`(?i)\b(?:(?:every|any|all|each)\s+(?:user\s+)?(?:messages?|responses?|replies|reply|turns?)\b|always\s+(?:applies|apply|active|on|in effect|use this|load)\b)`)

// AppliesAlways reports whether a skill's description says it applies to
// every message.
func AppliesAlways(description string) bool {
	return alwaysPattern.MatchString(description)
}

// inlineSkills is the bodies of the skills that apply to every message, up
// to skillsMax; a skill that does not fit is named so the model loads it.
func inlineSkills(skills []Skill) string {
	var b strings.Builder
	var inlined, left []string
	for _, s := range skills {
		if !AppliesAlways(s.Description) {
			continue
		}
		data, err := os.ReadFile(s.Path)
		if err != nil {
			continue
		}
		body := skillBody(string(data))
		if len(body) > skillMax || b.Len()+len(body) > skillsMax {
			left = append(left, s.Name)

			continue
		}
		inlined = append(inlined, s.Name)
		fmt.Fprintf(&b, "\n\n### Skill %s (%s)\n%s", s.Name, s.Path, body)
	}
	var out []string
	if len(inlined) > 0 {
		out = append(out, "Skills that apply to every message, inlined here so you need not load them: "+strings.Join(inlined, ", ")+"."+b.String())
	}
	if len(left) > 0 {
		out = append(out, "Skills that apply to every message but are too long to inline; load them: "+strings.Join(left, ", ")+".")
	}

	return strings.Join(out, "\n\n")
}

// skillBody is a SKILL.md without its front matter.
func skillBody(text string) string {
	text = strings.TrimSpace(text)
	if rest, ok := strings.CutPrefix(text, "---\n"); ok {
		if _, body, ok := strings.Cut(rest, "\n---"); ok {
			text = body
		}
	}

	return strings.TrimSpace(text)
}

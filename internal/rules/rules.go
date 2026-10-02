// Package rules is Codex's command rules: `prefix_rule` entries in Starlark
// `.rules` files, matched against a command's words, with the strictest
// matching decision winning.
package rules

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// Decision is what a rule says about a command.
type Decision int

// Decisions, from the least to the most strict.
const (
	Allow Decision = iota + 1
	Prompt
	Forbidden
)

// ParseDecision reads Codex's decision names.
func ParseDecision(s string) (Decision, error) {
	switch s {
	case "allow":
		return Allow, nil
	case "prompt":
		return Prompt, nil
	case "forbidden":
		return Forbidden, nil
	}

	return 0, fmt.Errorf("invalid decision %q (want allow, prompt, or forbidden)", s)
}

func (d Decision) String() string {
	switch d {
	case Allow:
		return "allow"
	case Prompt:
		return "prompt"
	case Forbidden:
		return "forbidden"
	}

	return "none"
}

// Rule is one prefix rule. Each pattern position lists the words it
// accepts.
type Rule struct {
	Pattern       [][]string
	Decision      Decision
	Justification string
	// Source is where the rule came from, such as a file path.
	Source string
}

// Matches reports whether the rule's pattern is a prefix of the words. A
// first word that is an absolute path also matches by its base name, as
// Codex does when no host_executable entry restricts it.
func (r Rule) Matches(words []string) bool {
	if len(words) < len(r.Pattern) || len(r.Pattern) == 0 {
		return false
	}
	for i, alts := range r.Pattern {
		w := words[i]
		if slices.Contains(alts, w) {
			continue
		}
		if i == 0 && filepath.IsAbs(w) && slices.Contains(alts, filepath.Base(w)) {
			continue
		}

		return false
	}

	return true
}

// mayMatch reports whether words the rule's pattern does not cover yet
// could complete a match: the known words agree with the pattern so far.
func (r Rule) mayMatch(words []string) bool {
	if len(words) >= len(r.Pattern) {
		return r.Matches(words)
	}
	for i, w := range words {
		alts := r.Pattern[i]
		if !slices.Contains(alts, w) && (i != 0 || !filepath.IsAbs(w) || !slices.Contains(alts, filepath.Base(w))) {
			return false
		}
	}

	return true
}

// String is the rule as a line of a rules file.
func (r Rule) String() string {
	parts := make([]string, 0, len(r.Pattern))
	for _, alts := range r.Pattern {
		if len(alts) == 1 {
			parts = append(parts, quote(alts[0]))

			continue
		}
		q := make([]string, 0, len(alts))
		for _, a := range alts {
			q = append(q, quote(a))
		}
		parts = append(parts, "["+strings.Join(q, ", ")+"]")
	}
	line := fmt.Sprintf("prefix_rule(pattern=[%s], decision=%q", strings.Join(parts, ", "), r.Decision.String())
	if r.Justification != "" {
		line += ", justification=" + quote(r.Justification)
	}

	return line + ")"
}

// Policy is a set of rules.
type Policy struct {
	rules []Rule
}

// New returns a policy with the rules.
func New(rules ...Rule) *Policy {
	return &Policy{rules: slices.Clone(rules)}
}

// Rules returns the policy's rules.
func (p *Policy) Rules() []Rule {
	if p == nil {
		return nil
	}

	return slices.Clone(p.rules)
}

// With returns a policy with the rules added; p is unchanged.
func (p *Policy) With(rules ...Rule) *Policy {
	return New(slices.Concat(p.Rules(), rules)...)
}

// Match returns the strictest rule matching the words.
func (p *Policy) Match(words []string) (Rule, bool) {
	var best Rule
	found := false
	for _, r := range p.Rules() {
		if r.Matches(words) && (!found || r.Decision > best.Decision) {
			best, found = r, true
		}
	}

	return best, found
}

// Check evaluates a command made of several simple commands, such as
// `a && b | c`. Forbidden or prompt wins when any command matches it;
// allow needs every command to match an allow rule. ok is false when no
// decision applies, so the default policy does.
func (p *Policy) Check(commands [][]string) (Rule, bool) {
	var strictest Rule
	allowed := len(commands) > 0
	for _, words := range commands {
		r, ok := p.Match(words)
		if !ok {
			allowed = false

			continue
		}
		if r.Decision > strictest.Decision {
			strictest = r
		}
	}
	if strictest.Decision == Allow && !allowed {
		return Rule{}, false
	}

	return strictest, strictest.Decision != 0
}

// Forbids finds a forbidden rule for a command that Split does not reduce
// to plain words, so that Check never sees it: one with redirects,
// assignments, variables, globs, subshells, substitutions, or control
// flow. It checks every simple command inside. found is true when a
// simple command matches a forbidden rule, or may match one because a
// word the rule needs is not plain text, such as `rm $FLAGS` for a rule on
// `rm -rf`; maybe is true in that second case. A command that does not
// parse may match any forbidden rule.
func (p *Policy) Forbids(command string) (r Rule, maybe, found bool) {
	var forbidden []Rule
	for _, r := range p.Rules() {
		if r.Decision == Forbidden {
			forbidden = append(forbidden, r)
		}
	}
	if len(forbidden) == 0 {
		return Rule{}, false, false
	}
	commands, ok := scan(command)
	if !ok {
		return forbidden[0], true, true
	}
	for _, c := range commands {
		for _, r := range forbidden {
			if r.Matches(c.words) {
				return r, false, true
			}
		}
	}
	for _, c := range commands {
		if !c.partial {
			continue
		}
		for _, r := range forbidden {
			if r.mayMatch(c.words) {
				return r, true, true
			}
		}
	}

	return Rule{}, false, false
}

// quote writes a string as a Starlark string literal.
func quote(s string) string {
	return fmt.Sprintf("%q", s)
}

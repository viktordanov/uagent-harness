package rules

import (
	"errors"
	"fmt"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// Parse reads a rules file with Starlark. It knows Codex's prefix_rule and
// accepts host_executable and network_rule without applying them, so Codex
// rules files load unchanged. A rule's match and not_match examples are
// checked, as Codex checks them.
func Parse(filename string, src []byte) ([]Rule, error) {
	var rules []Rule
	prefixRule := func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		r, err := parseRule(b.Name(), args, kwargs)
		if err != nil {
			return nil, err
		}
		r.Source = filename
		rules = append(rules, r)

		return starlark.None, nil
	}
	ignored := func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
		return starlark.None, nil
	}
	predeclared := starlark.StringDict{
		"prefix_rule":     starlark.NewBuiltin("prefix_rule", prefixRule),
		"host_executable": starlark.NewBuiltin("host_executable", ignored),
		"network_rule":    starlark.NewBuiltin("network_rule", ignored),
	}
	thread := &starlark.Thread{Name: filename}
	if _, err := starlark.ExecFileOptions(&syntax.FileOptions{}, thread, filename, src, predeclared); err != nil {
		return nil, fmt.Errorf("failed to read rules: %w", err)
	}

	return rules, nil
}

func parseRule(name string, args starlark.Tuple, kwargs []starlark.Tuple) (Rule, error) {
	var (
		pattern         *starlark.List
		decision        = "allow"
		justification   string
		match, notMatch *starlark.List
	)
	if err := starlark.UnpackArgs(name, args, kwargs,
		"pattern", &pattern, "decision?", &decision, "justification?", &justification,
		"match?", &match, "not_match?", &notMatch); err != nil {
		return Rule{}, err //nolint:wrapcheck // Starlark's message names the call
	}
	d, err := ParseDecision(decision)
	if err != nil {
		return Rule{}, err
	}
	if _, set := kwarg(kwargs, "justification"); set && strings.TrimSpace(justification) == "" {
		return Rule{}, errors.New("justification cannot be empty")
	}
	r := Rule{Decision: d, Justification: justification}
	if r.Pattern, err = parsePattern(pattern); err != nil {
		return Rule{}, err
	}

	return r, checkExamples(r, match, notMatch)
}

func kwarg(kwargs []starlark.Tuple, name string) (starlark.Value, bool) {
	for _, kv := range kwargs {
		if s, ok := kv[0].(starlark.String); ok && string(s) == name {
			return kv[1], true
		}
	}

	return nil, false
}

// parsePattern reads a list of words, where a nested list is alternatives.
func parsePattern(list *starlark.List) ([][]string, error) {
	if list.Len() == 0 {
		return nil, errors.New("pattern cannot be empty")
	}
	out := make([][]string, 0, list.Len())
	for i := range list.Len() {
		switch v := list.Index(i).(type) {
		case starlark.String:
			out = append(out, []string{string(v)})
		case *starlark.List:
			alts, err := stringList(v)
			if err != nil || len(alts) == 0 {
				return nil, errors.New("pattern alternatives must be a non-empty list of strings")
			}
			out = append(out, alts)
		default:
			return nil, fmt.Errorf("pattern element %d must be a string or a list of strings, not %s", i, v.Type())
		}
	}

	return out, nil
}

// checkExamples checks that the rule matches every match example and none
// of the not_match examples. An example is a list of words or a string.
func checkExamples(r Rule, match, notMatch *starlark.List) error {
	for want, list := range map[bool]*starlark.List{true: match, false: notMatch} {
		if list == nil {
			continue
		}
		for i := range list.Len() {
			words, err := example(list.Index(i))
			if err != nil {
				return err
			}
			if r.Matches(words) != want {
				verb := "match"
				if !want {
					verb = "not match"
				}

				return fmt.Errorf("the rule %s should %s its example %q", r, verb, strings.Join(words, " "))
			}
		}
	}

	return nil
}

func example(v starlark.Value) ([]string, error) {
	switch e := v.(type) {
	case starlark.String:
		words, ok := Words(string(e))
		if !ok {
			return nil, fmt.Errorf("example %q is not a simple command", string(e))
		}

		return words, nil
	case *starlark.List:
		words, err := stringList(e)
		if err != nil || len(words) == 0 {
			return nil, errors.New("an example must be a non-empty list of strings")
		}

		return words, nil
	}

	return nil, fmt.Errorf("an example must be a string or a list, not %s", v.Type())
}

func stringList(list *starlark.List) ([]string, error) {
	out := make([]string, 0, list.Len())
	for i := range list.Len() {
		s, ok := list.Index(i).(starlark.String)
		if !ok {
			return nil, fmt.Errorf("want a string, not %s", list.Index(i).Type())
		}
		out = append(out, string(s))
	}

	return out, nil
}

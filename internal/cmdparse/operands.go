// Adapted from openai/codex rust-v0.159.1 (Apache License 2.0, Copyright
// 2025 OpenAI): codex-rs/shell-command/src/parse_command.rs (the operand
// helpers and the per-command parsers).

package cmdparse

import (
	"slices"
	"strings"
)

// skipFlagValues drops the values of flags that take one, and
// --flag=value arguments; everything after -- is an operand.
func skipFlagValues(args, flagsWithValues []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return append(out, args[i+1:]...)
		case strings.HasPrefix(a, "--") && strings.Contains(a, "="):
		case slices.Contains(flagsWithValues, a):
			if i+1 < len(args) {
				i++
			}
		default:
			out = append(out, a)
		}
	}

	return out
}

// positionalOperands are the arguments that are neither flags nor their
// values.
func positionalOperands(args, flagsWithValues []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return append(out, args[i+1:]...)
		case strings.HasPrefix(a, "--") && strings.Contains(a, "="):
		case slices.Contains(flagsWithValues, a):
			if i+1 < len(args) {
				i++
			}
		case strings.HasPrefix(a, "-"):
		default:
			out = append(out, a)
		}
	}

	return out
}

func firstNonFlagOperand(args, flagsWithValues []string) (string, bool) {
	ops := positionalOperands(args, flagsWithValues)
	if len(ops) == 0 {
		return "", false
	}

	return ops[0], true
}

func singleNonFlagOperand(args, flagsWithValues []string) (string, bool) {
	ops := positionalOperands(args, flagsWithValues)
	if len(ops) != 1 {
		return "", false
	}

	return ops[0], true
}

// flagValues are the values given to any of flags (uah: rg's globs).
func flagValues(args []string, flags ...string) []string {
	var out []string
	for i, a := range args {
		if slices.Contains(flags, a) && i+1 < len(args) {
			out = append(out, args[i+1])
		}
	}

	return out
}

// parseGrepLike is grep's (and git grep's) search: the pattern from -e
// or -f, else the first operand, and the path after it. The query keeps
// its slashes; only the path is shortened.
func parseGrepLike(main, args []string) []Parsed {
	var operands []string
	pattern, hasPattern := "", false
	args = trimAtConnector(args)
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--":
			operands = append(operands, args[i+1:]...)
			i = len(args)

			continue
		case "-e", "--regexp", "-f", flagFile:
			if i+1 < len(args) && !hasPattern {
				pattern, hasPattern = args[i+1], true
			}
			i++

			continue
		case "-m", "--max-count", "-C", "--context", "-A", "--after-context", "-B", "--before-context":
			i++

			continue
		}
		if !strings.HasPrefix(a, "-") {
			operands = append(operands, a)
		}
	}
	p := Parsed{Kind: Search, Cmd: join(main)}
	pathIndex := 1
	if hasPattern {
		p.Query, pathIndex = pattern, 0
	} else if len(operands) > 0 {
		p.Query = operands[0]
	}
	if pathIndex < len(operands) {
		p.Path, p.Paths = shortDisplayPath(operands[pathIndex]), operands[pathIndex:]
	}

	return []Parsed{p}
}

// awkDataFileOperand is the file awk reads: the first operand after a
// -f script file, else the second operand (the first is the program).
func awkDataFileOperand(args []string) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	args = trimAtConnector(args)
	hasScriptFile := contains(args, "-f") || contains(args, flagFile)
	var nonFlags []string
	for _, a := range skipFlagValues(args, []string{"-F", "-v", "-f", "--field-separator", "--assign", flagFile}) {
		if !strings.HasPrefix(a, "-") {
			nonFlags = append(nonFlags, a)
		}
	}
	switch {
	case hasScriptFile && len(nonFlags) > 0:
		return nonFlags[0], true
	case !hasScriptFile && len(nonFlags) >= 2:
		return nonFlags[1], true
	}

	return "", false
}

// pythonWalksFiles reports a python -c script that walks the file tree.
func pythonWalksFiles(args []string) bool {
	args = trimAtConnector(args)
	for i, a := range args {
		if a != "-c" || i+1 >= len(args) {
			continue
		}
		script := args[i+1]
		for _, s := range []string{"os.walk", "os.listdir", "os.scandir", "glob.glob", "glob.iglob", "pathlib.Path", ".rglob("} {
			if strings.Contains(script, s) {
				return true
			}
		}

		return false
	}

	return false
}

func isPythonCommand(cmd string) bool {
	return cmd == "python" || cmd == "python2" || cmd == "python3" || strings.HasPrefix(cmd, "python2.") || strings.HasPrefix(cmd, "python3.")
}

// isPathish reports a word with an explicit path shape.
func isPathish(s string) bool {
	return s == "." || s == ".." || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") || strings.ContainsAny(s, `/\`)
}

// fdQueryAndPath: fd's one operand is a path when it looks like one,
// else a query; with two, a query and a path.
func fdQueryAndPath(tail []string) (query, dir string) {
	var nonFlags []string
	for _, a := range skipFlagValues(trimAtConnector(tail), []string{"-t", "--type", "-e", "--extension", "-E", flagExclude, "--search-path"}) {
		if !strings.HasPrefix(a, "-") {
			nonFlags = append(nonFlags, a)
		}
	}
	switch {
	case len(nonFlags) == 1 && isPathish(nonFlags[0]):
		return "", nonFlags[0]
	case len(nonFlags) == 1:
		return nonFlags[0], ""
	case len(nonFlags) >= 2:
		return nonFlags[0], nonFlags[1]
	}

	return "", ""
}

// findQueryAndPath: find's root is its first operand, and its query the
// value of -name, -iname, -path, or -regex.
func findQueryAndPath(tail []string) (query, dir string) {
	args := trimAtConnector(tail)
	for _, a := range args {
		if !strings.HasPrefix(a, "-") && a != "!" && a != "(" && a != ")" {
			dir = a

			break
		}
	}
	for i, a := range args {
		if a == "-name" || a == "-iname" || a == "-path" || a == "-regex" {
			if i+1 < len(args) {
				query = args[i+1]
			}

			break
		}
	}

	return query, dir
}
